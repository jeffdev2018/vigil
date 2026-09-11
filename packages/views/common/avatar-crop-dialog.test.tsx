// @vitest-environment jsdom
import { useEffect } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../locales/en/common.json";
import { AvatarCropDialog } from "./avatar-crop-dialog";

const TEST_RESOURCES = { en: { common: enCommon } };

// react-easy-crop drives real canvas geometry the jsdom test environment
// cannot render; stub it down to something that reports a crop area once on
// mount (via effect, not during render — calling the setState-driving prop
// synchronously during render would re-trigger a render every time), which
// is all this dialog's chrome needs to enable Save.
vi.mock("react-easy-crop", () => ({
  default: ({ onCropComplete }: { onCropComplete: (a: unknown, b: unknown) => void }) => {
    useEffect(() => {
      onCropComplete({}, { x: 0, y: 0, width: 10, height: 10 });
      // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);
    return <div data-testid="cropper-stub" />;
  },
}));

const mockGetCroppedAvatarBlob = vi.hoisted(() => vi.fn());
vi.mock("./avatar-crop", async (importOriginal) => {
  const actual = await importOriginal<typeof import("./avatar-crop")>();
  return {
    ...actual,
    getCroppedAvatarBlob: (...args: unknown[]) => mockGetCroppedAvatarBlob(...args),
    pickOutputType: () => ({ type: "image/webp", quality: 0.85 }),
  };
});

const FILE = new File(["x"], "avatar.png", { type: "image/png" });

function renderDialog(onCropped = vi.fn()) {
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <AvatarCropDialog file={FILE} open onOpenChange={() => {}} onCropped={onCropped} />
    </I18nProvider>,
  );
}

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("AvatarCropDialog", () => {
  it("hands the cropped file to the parent on a successful encode", async () => {
    mockGetCroppedAvatarBlob.mockResolvedValue(new Blob(["x"], { type: "image/webp" }));
    const onCropped = vi.fn();
    renderDialog(onCropped);

    fireEvent.click(screen.getByText(enCommon.avatar_crop.apply));
    await vi.waitFor(() => expect(onCropped).toHaveBeenCalled());
  });

  // Audit finding (P2): getCroppedAvatarBlob failing (e.g. canvas.toBlob)
  // was swallowed by an empty catch that reused the load_failed copy
  // ("Couldn't load image") even though the image loaded fine — only the
  // encode step broke — and it locked the dialog with no way to retry.
  it("logs and shows a dedicated retry-able error when the crop encode fails, distinct from load_failed", async () => {
    mockGetCroppedAvatarBlob.mockRejectedValue(new Error("canvas.toBlob failed"));
    const consoleError = vi.spyOn(console, "error").mockImplementation(() => {});
    const onCropped = vi.fn();
    renderDialog(onCropped);

    fireEvent.click(screen.getByText(enCommon.avatar_crop.apply));

    expect(await screen.findByText(enCommon.avatar_crop.encode_failed)).toBeTruthy();
    expect(screen.queryByText(enCommon.avatar_crop.load_failed)).toBeNull();
    expect(onCropped).not.toHaveBeenCalled();
    expect(consoleError).toHaveBeenCalled();
    // The cropper stays mounted (not replaced by the load_failed dead end),
    // so retrying is just clicking Save again.
    expect(screen.getByTestId("cropper-stub")).toBeTruthy();

    consoleError.mockRestore();
  });
});
