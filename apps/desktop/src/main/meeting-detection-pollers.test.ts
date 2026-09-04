// @vitest-environment node
import { describe, expect, it, vi } from "vitest";
import { pickMeetingOwner } from "./meeting-detection";
import {
  canonicalBinaryName,
  parsePactlSourceOutputs,
  parseWindowsMicConsent,
  startMicPoller,
} from "./meeting-detection-pollers";

const PACTL = `Source Output #12
\tDriver: protocol-native.c
\tOwner Module: 8
\tClient: 45
\tSource: 1 <alsa_input.pci-0000_00_1f.3.analog-stereo>
\tProperties:
\t\tmedia.name = "recStream"
\t\tapplication.name = "ZOOM VoiceEngine"
\t\tapplication.process.id = "4242"
\t\tapplication.process.binary = "zoom"
Source Output #13
\tDriver: protocol-native.c
\tSource: 3 <alsa_output.pci-0000_00_1f.3.analog-stereo.monitor>
\tProperties:
\t\tapplication.name = "Chromium input"
\t\tapplication.process.id = "777"
\t\tapplication.process.binary = "electron"
Source Output #14
\tSource: 1 <alsa_input.pci-0000_00_1f.3.analog-stereo>
\tProperties:
\t\tapplication.name = "Chromium input"
\t\tapplication.process.id = "900"
\t\tapplication.process.binary = "chrome"
`;

const REG = `
HKEY_CURRENT_USER\\Software\\Microsoft\\Windows\\CurrentVersion\\CapabilityAccessManager\\ConsentStore\\microphone
    Value    REG_SZ    Allow

HKEY_CURRENT_USER\\Software\\Microsoft\\Windows\\CurrentVersion\\CapabilityAccessManager\\ConsentStore\\microphone\\Microsoft.WindowsCamera_8wekyb3d8bbwe
    LastUsedTimeStart    REG_QWORD    0x1dc1e3f4a2b3c4d
    LastUsedTimeStop    REG_QWORD    0x1dc1e3f4b0000000

HKEY_CURRENT_USER\\Software\\Microsoft\\Windows\\CurrentVersion\\CapabilityAccessManager\\ConsentStore\\microphone\\NonPackaged
    Value    REG_SZ    Allow

HKEY_CURRENT_USER\\Software\\Microsoft\\Windows\\CurrentVersion\\CapabilityAccessManager\\ConsentStore\\microphone\\NonPackaged\\C:#Users#jeff#AppData#Roaming#Zoom#bin#Zoom.exe
    LastUsedTimeStart    REG_QWORD    0x1dc1e3f4a2b3c4d
    LastUsedTimeStop    REG_QWORD    0x0

HKEY_CURRENT_USER\\Software\\Microsoft\\Windows\\CurrentVersion\\CapabilityAccessManager\\ConsentStore\\microphone\\NonPackaged\\C:#Program Files#Google#Chrome#Application#chrome.exe
    LastUsedTimeStart    REG_QWORD    0x1dc1e3f4a2b3c4d
    LastUsedTimeStop    REG_QWORD    0x1dc1e3f4a3000000
`;

describe("parsePactlSourceOutputs", () => {
  it("reports recording streams by canonical binary name and skips monitors", () => {
    const owners = parsePactlSourceOutputs(PACTL);
    expect(owners).toEqual([
      { pid: 4242, bundleId: "", path: "zoom.us" },
      { pid: 900, bundleId: "", path: "Google Chrome" },
    ]);
    // The matcher prefers the dedicated conferencing app over a browser.
    expect(pickMeetingOwner(owners)?.appName).toBe("Zoom");
  });

  it("returns nothing when no stream records", () => {
    expect(parsePactlSourceOutputs("")).toEqual([]);
  });
});

describe("parseWindowsMicConsent", () => {
  it("keeps only apps whose last use has not stopped", () => {
    const owners = parseWindowsMicConsent(REG);
    expect(owners).toEqual([{ pid: 0, bundleId: "", path: "zoom.us" }]);
    expect(pickMeetingOwner(owners)?.appName).toBe("Zoom");
  });

  it("ignores the store keys themselves and stopped apps", () => {
    const stopped = REG.replace("LastUsedTimeStop    REG_QWORD    0x0", "LastUsedTimeStop    REG_QWORD    0x1dc1e3f4a9999999");
    expect(parseWindowsMicConsent(stopped)).toEqual([]);
  });
});

describe("canonicalBinaryName", () => {
  it("maps OS binaries onto matcher names and leaves unknown ones alone", () => {
    expect(canonicalBinaryName("Zoom.exe")).toBe("zoom.us");
    expect(canonicalBinaryName("teams-for-linux")).toBe("MSTeams");
    expect(canonicalBinaryName("obscure-tool")).toBe("obscure-tool");
  });
});

describe("startMicPoller", () => {
  it("feeds parsed readings and disables itself after repeated failures", async () => {
    vi.useFakeTimers();
    const readings: boolean[] = [];
    let calls = 0;
    const disabled = vi.fn();
    const stop = startMicPoller({
      platform: "linux",
      intervalMs: 10,
      run: async () => {
        calls += 1;
        if (calls > 2) throw new Error("pactl: not found");
        return calls === 1 ? PACTL : "";
      },
      onReading: (r) => readings.push(r.micInUse),
      onDisabled: disabled,
    });
    await vi.advanceTimersByTimeAsync(5);
    await vi.advanceTimersByTimeAsync(10);
    await vi.advanceTimersByTimeAsync(10);
    await vi.advanceTimersByTimeAsync(10);
    await vi.advanceTimersByTimeAsync(10);
    expect(readings).toEqual([true, false]);
    expect(disabled).toHaveBeenCalledTimes(1);
    stop();
    vi.useRealTimers();
  });

  it("is a no-op on platforms without a source", () => {
    const disabled = vi.fn();
    const stop = startMicPoller({ platform: "darwin", onReading: () => {}, onDisabled: disabled });
    expect(disabled).toHaveBeenCalled();
    stop();
  });
});
