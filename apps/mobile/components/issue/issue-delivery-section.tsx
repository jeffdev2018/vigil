/**
 * Human delivery review on the issue detail header.
 *
 * Product semantics mirror packages/views/issues/components/issue-delivery-section.tsx:
 * honesty copy, board In Review ≠ accept ≠ merge/deploy, assessments+evidence
 * before accept, 409 conflicts, correction launch, optional Done after accept.
 *
 * UI differs for phone: inline review form (no Dialog), Alert for propose Done,
 * no cost dialog / full history / teach-memory / transcript chrome in this cut.
 */
import { useState } from "react";
import { Alert, Linking, Pressable, View } from "react-native";
import { useQuery } from "@tanstack/react-query";
import type { Issue } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { TextField } from "@/components/ui/text-field";
import { issueDeliveryOptions } from "@/data/queries/delivery";
import {
  deliveryCorrectionErrorMessage,
  deliverySaveErrorMessage,
  useReviewDelivery,
  useStartDeliveryCorrection,
  useUpdateDeliveryCriteria,
  type ReviewDeliveryInput,
} from "@/data/mutations/delivery";
import { useUpdateIssue } from "@/data/mutations/issues";
import { useWorkspaceStore } from "@/data/workspace-store";
import {
  issueBehavesAs,
  issueBehavesAsAny,
  CLOSED_CATEGORIES,
} from "@/lib/issue-status";

type ReviewDraft = ReviewDeliveryInput & { criteria: string[]; startedAtMs: number };

function newReviewId(): string {
  if (typeof globalThis.crypto?.randomUUID === "function") {
    return globalThis.crypto.randomUUID();
  }
  return "xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx".replace(/[xy]/g, (c) => {
    const r = (Math.random() * 16) | 0;
    const v = c === "x" ? r : (r & 0x3) | 0x8;
    return v.toString(16);
  });
}

function resultText(result: unknown): string {
  if (result == null) return "No result was reported.";
  const summary =
    typeof result === "object" && result !== null && "summary" in result
      ? (result as { summary: unknown }).summary
      : result;
  return typeof summary === "string" ? summary : JSON.stringify(result, null, 2);
}

export function IssueDeliverySection({ issue }: { issue: Issue }) {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { data, isPending, isError, refetch } = useQuery(
    issueDeliveryOptions(wsId, issue.id),
  );
  const criteriaMutation = useUpdateDeliveryCriteria(issue.id);
  const reviewMutation = useReviewDelivery(issue.id);
  const correctionMutation = useStartDeliveryCorrection(issue.id);
  const updateIssue = useUpdateIssue(issue.id);

  const [criteriaDraft, setCriteriaDraft] = useState<{
    text: string;
    revision: number;
  } | null>(null);
  const [reviewDraft, setReviewDraft] = useState<ReviewDraft | null>(null);
  const [error, setError] = useState<string | null>(null);

  const boardStatusIsReview = issueBehavesAs(issue, "in_review");
  const canProposeDone = !issueBehavesAsAny(issue, CLOSED_CATEGORIES);

  const criteriaLines = (criteriaDraft?.text ?? "")
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(Boolean);
  const validCriteria =
    criteriaLines.length <= 20 &&
    criteriaLines.every((criterion) => [...criterion].length <= 500);
  const busy =
    criteriaMutation.isPending ||
    reviewMutation.isPending ||
    correctionMutation.isPending ||
    updateIssue.isPending;
  const draftStale =
    reviewDraft !== null &&
    data !== undefined &&
    (reviewDraft.snapshotToken !== data.snapshotToken ||
      reviewDraft.expectedReviewId !== (data.latestReview?.id ?? ""));
  const unavailable = isError || (!isPending && !data);
  const runReady =
    data?.run?.status === "completed" &&
    Boolean(data.run.completedAt) &&
    data.criteria.length > 0;
  const needsHumanDecision = Boolean(
    runReady &&
      data &&
      (data.reviewStale ||
        data.latestReview == null ||
        data.latestReview.decision !== "accepted"),
  );
  const needsCorrectionLaunch = Boolean(
    data?.latestReview?.decision === "changes_requested" &&
      !data.latestReview.correctionTaskId &&
      !data.reviewStale &&
      data.latestReview.snapshotToken === data.snapshotToken,
  );

  const startReview = () => {
    if (!data) return;
    setError(null);
    setReviewDraft({
      reviewId: newReviewId(),
      expectedReviewId: data.latestReview?.id ?? "",
      snapshotToken: data.snapshotToken,
      decision: "accepted",
      feedback: "",
      criteria: [...data.criteria],
      assessments: data.criteria.map(() => ({ passed: false, evidence: "" })),
      startedAtMs: Date.now(),
    });
  };

  const proposeDone = () => {
    Alert.alert(
      "Mark the issue Done?",
      "Acceptance is recorded. Updating the board status is optional and separate — it still does not authorize a merge or deployment.",
      [
        { text: "Keep current status", style: "cancel" },
        {
          text: "Mark as Done",
          onPress: () => updateIssue.mutate({ status: "done" }),
        },
      ],
    );
  };

  const decide = (decision: ReviewDeliveryInput["decision"]) => {
    if (!reviewDraft || draftStale) return;
    setError(null);
    const humanEffortSeconds = Math.min(
      24 * 60 * 60,
      Math.max(0, Math.round((Date.now() - reviewDraft.startedAtMs) / 1000)),
    );
    const { criteria: _criteria, startedAtMs: _startedAtMs, ...input } =
      reviewDraft;
    reviewMutation.mutate(
      { ...input, decision, humanEffortSeconds },
      {
        onSuccess: () => {
          setReviewDraft(null);
          if (decision === "accepted" && canProposeDone) proposeDone();
        },
        onError: (err) => setError(deliverySaveErrorMessage(err)),
      },
    );
  };

  const acceptReady =
    reviewDraft !== null &&
    !draftStale &&
    reviewDraft.assessments.length === reviewDraft.criteria.length &&
    reviewDraft.assessments.every(
      (a) => a.passed === true && a.evidence.trim().length > 0,
    );

  return (
    <View
      className="mx-4 mt-4 mb-2 rounded-lg border border-border bg-card p-4 gap-3"
      accessibilityLabel="Delivery review"
    >
      <Text className="text-sm font-medium text-foreground">
        Delivery review
      </Text>
      <Text className="text-xs text-muted-foreground">
        Review the reported result against your criteria. Completion alone does
        not mean acceptance.
      </Text>
      <Text className="text-xs text-muted-foreground">
        Board status (including In Review), delivery acceptance, and merge or
        deploy permission are separate. Accepting here records a human
        assessment only — it does not authorize a merge or deployment.
      </Text>

      {boardStatusIsReview ? (
        <View
          className="rounded-md border border-border/60 bg-muted/40 px-3 py-2"
          accessibilityRole="text"
        >
          <Text className="text-xs text-muted-foreground">
            This issue is In Review on the board. That is a workflow signal
            only: it does not mean the delivery was accepted, and it does not
            block merge or deploy.
          </Text>
        </View>
      ) : null}

      {needsHumanDecision && !unavailable && data ? (
        <View className="rounded-md border border-border bg-muted/50 px-3 py-2 gap-2">
          <Text className="text-xs text-foreground">
            {needsCorrectionLaunch
              ? "Corrections were requested. Start a correction run, then review its result."
              : "A completed run is waiting for your accept or correction decision."}
          </Text>
          {needsCorrectionLaunch ? (
            <Button
              size="sm"
              disabled={busy}
              onPress={() => {
                if (!data.latestReview) return;
                setError(null);
                correctionMutation.mutate(data.latestReview.id, {
                  onError: (err) =>
                    setError(deliveryCorrectionErrorMessage(err)),
                });
              }}
            >
              <Text>Start correction</Text>
            </Button>
          ) : (
            <Button
              size="sm"
              disabled={busy || !runReady || reviewDraft !== null}
              onPress={startReview}
            >
              <Text>Review delivery</Text>
            </Button>
          )}
        </View>
      ) : null}

      {isPending ? (
        <Text className="text-xs text-muted-foreground">Loading delivery…</Text>
      ) : unavailable ? (
        <View className="gap-2" accessibilityRole="alert">
          <Text className="text-xs text-destructive">
            Delivery evidence could not be loaded.
          </Text>
          <Button
            size="sm"
            variant="outline"
            onPress={() => void refetch()}
          >
            <Text>Reload</Text>
          </Button>
        </View>
      ) : data ? (
        <>
          <View className="flex-row items-center justify-between gap-2">
            <Text className="text-xs font-medium text-foreground">
              Acceptance criteria
            </Text>
            {!criteriaDraft ? (
              <Button
                size="sm"
                variant="outline"
                disabled={busy}
                onPress={() => {
                  setError(null);
                  setCriteriaDraft({
                    text: data.criteria.join("\n"),
                    revision: data.revision,
                  });
                }}
              >
                <Text>Edit criteria</Text>
              </Button>
            ) : null}
          </View>

          {criteriaDraft ? (
            <View className="gap-2">
              <Text className="text-xs text-muted-foreground">
                One criterion per line. Up to 20 criteria, 500 characters each.
                Changing criteria makes previous reviews outdated.
              </Text>
              <TextField
                value={criteriaDraft.text}
                editable={!busy}
                onChangeText={(text) =>
                  setCriteriaDraft((d) => (d ? { ...d, text } : d))
                }
                multiline
                className="min-h-[88px] h-auto py-2"
                style={{ textAlignVertical: "top" }}
              />
              <View className="flex-row flex-wrap gap-2">
                <Button
                  size="sm"
                  disabled={busy || !validCriteria}
                  onPress={() => {
                    setError(null);
                    criteriaMutation.mutate(
                      {
                        criteria: criteriaLines,
                        expectedRevision: criteriaDraft.revision,
                      },
                      {
                        onSuccess: () => setCriteriaDraft(null),
                        onError: (err) =>
                          setError(deliverySaveErrorMessage(err)),
                      },
                    );
                  }}
                >
                  <Text>Save criteria</Text>
                </Button>
                <Button
                  size="sm"
                  variant="ghost"
                  disabled={busy}
                  onPress={() => setCriteriaDraft(null)}
                >
                  <Text>Cancel</Text>
                </Button>
              </View>
            </View>
          ) : data.criteria.length === 0 ? (
            <Text className="text-xs text-muted-foreground">
              Define what a satisfactory result must do before accepting it.
            </Text>
          ) : (
            <View className="gap-1.5">
              {data.criteria.map((criterion, index) => (
                <Text
                  key={`${index}-${criterion.slice(0, 24)}`}
                  className="text-sm text-foreground"
                >
                  {index + 1}. {criterion}
                </Text>
              ))}
            </View>
          )}

          {data.run ? (
            <View className="gap-1.5">
              <Text className="text-xs font-medium text-foreground">
                Reported result
              </Text>
              <Text className="text-xs text-muted-foreground">
                {data.run.completedAt
                  ? `Run ended ${new Date(data.run.completedAt).toLocaleString()}`
                  : "The latest run has not finished."}
              </Text>
              <Text className="text-sm text-foreground">
                {resultText(data.run.result)}
              </Text>
              {data.run.error ? (
                <Text className="text-xs text-destructive">
                  {data.run.error}
                </Text>
              ) : null}
            </View>
          ) : (
            <Text className="text-xs text-muted-foreground">
              No run to review yet.
            </Text>
          )}

          {data.pullRequests.length > 0 ? (
            <View className="gap-2">
              <Text className="text-xs font-medium text-foreground">
                Linked evidence
              </Text>
              {data.pullRequests.map((pr) => (
                <View key={pr.id} className="gap-0.5">
                  {/^https?:\/\//i.test(pr.html_url) ? (
                    <Pressable
                      onPress={() => void Linking.openURL(pr.html_url)}
                      accessibilityRole="link"
                    >
                      <Text className="text-sm text-primary underline">
                        {pr.title}
                      </Text>
                    </Pressable>
                  ) : (
                    <Text className="text-sm text-foreground">{pr.title}</Text>
                  )}
                  <Text className="text-xs text-muted-foreground">
                    {pr.headSha || "Commit unavailable"}
                  </Text>
                  <Text className="text-xs text-muted-foreground">
                    {pr.snapshot_available === false || pr.checks_total === 0
                      ? "Current check results unavailable"
                      : `${pr.checks_passed} of ${pr.checks_total} checks passed`}
                    {pr.snapshot_stale === true ? " · Last known checks are stale" : ""}
                  </Text>
                </View>
              ))}
            </View>
          ) : null}

          {data.latestReview ? (
            <View className="rounded-md bg-muted/50 p-3 gap-1.5">
              <Text className="text-xs font-medium text-foreground">
                {data.reviewStale
                  ? "Previous review is outdated"
                  : data.latestReview.decision === "accepted"
                    ? "Accepted by a human"
                    : "Corrections requested"}
              </Text>
              <Text className="text-xs text-muted-foreground">
                Reviewed {new Date(data.latestReview.createdAt).toLocaleString()}
              </Text>
              {data.latestReview.reviewDelaySeconds != null ? (
                <Text className="text-xs text-muted-foreground">
                  Recorded{" "}
                  {(data.latestReview.reviewDelaySeconds / 60).toLocaleString(
                    undefined,
                    { maximumFractionDigits: 1 },
                  )}{" "}
                  min after run completion (elapsed time, not review work).
                </Text>
              ) : null}
              {data.latestReview.humanEffortSeconds != null ? (
                <Text className="text-xs text-muted-foreground">
                  Active review time from this client:{" "}
                  {data.latestReview.humanEffortSeconds.toLocaleString()} s
                  (self-timed while the review form was open).
                </Text>
              ) : null}
              {data.latestReview.feedback ? (
                <Text className="text-sm text-foreground">
                  {data.latestReview.feedback}
                </Text>
              ) : null}
              {data.latestReview.decision === "changes_requested" ? (
                data.latestReview.correctionTaskId ? (
                  <Text className="text-xs text-foreground" accessibilityRole="text">
                    A correction run was created. Its result will need a new
                    human review.
                  </Text>
                ) : !needsCorrectionLaunch ? (
                  <View className="gap-2">
                    <Text className="text-xs text-muted-foreground">
                      Start a correction with the agent that produced this
                      result. It receives this feedback and reuses the previous
                      work when available.
                    </Text>
                    <Button
                      size="sm"
                      variant="outline"
                      disabled={
                        busy ||
                        data.reviewStale ||
                        data.latestReview.snapshotToken !== data.snapshotToken
                      }
                      onPress={() => {
                        if (!data.latestReview) return;
                        setError(null);
                        correctionMutation.mutate(data.latestReview.id, {
                          onError: (err) =>
                            setError(deliveryCorrectionErrorMessage(err)),
                        });
                      }}
                    >
                      <Text>Start correction</Text>
                    </Button>
                  </View>
                ) : null
              ) : null}
            </View>
          ) : null}

          {reviewDraft ? (
            <View className="gap-3 border-t border-border pt-3">
              <Text className="text-xs font-medium text-foreground">
                Review delivery
              </Text>
              <Text className="text-xs text-muted-foreground">
                Confirm each criterion and record what you checked. This records
                your assessment; it does not authorize a merge or deployment.
                Moving the issue to In Review or Done is a separate board
                action.
              </Text>
              {draftStale ? (
                <Text className="text-xs text-destructive">
                  The delivery or review changed. Close this draft and reload
                  before deciding. Your draft has been kept.
                </Text>
              ) : null}
              {reviewDraft.criteria.map((criterion, index) => {
                const assessment = reviewDraft.assessments[index];
                return (
                  <View key={`${index}-${criterion.slice(0, 24)}`} className="gap-2">
                    <Pressable
                      disabled={busy || draftStale}
                      accessibilityRole="checkbox"
                      accessibilityState={{
                        checked: assessment?.passed === true,
                      }}
                      accessibilityLabel={criterion}
                      onPress={() =>
                        setReviewDraft((draft) => {
                          if (!draft) return draft;
                          const assessments = draft.assessments.map((row, i) =>
                            i === index
                              ? { ...row, passed: !row.passed }
                              : row,
                          );
                          return { ...draft, assessments };
                        })
                      }
                      className="flex-row items-start gap-2"
                    >
                      <View
                        className={`mt-0.5 h-5 w-5 rounded border items-center justify-center ${
                          assessment?.passed === true
                            ? "bg-primary border-primary"
                            : "border-border bg-background"
                        }`}
                      >
                        {assessment?.passed === true ? (
                          <Text className="text-xs text-primary-foreground">
                            ✓
                          </Text>
                        ) : null}
                      </View>
                      <Text className="flex-1 text-sm text-foreground">
                        {criterion}
                      </Text>
                    </Pressable>
                    <TextField
                      value={assessment?.evidence ?? ""}
                      editable={!busy && !draftStale}
                      onChangeText={(evidence) =>
                        setReviewDraft((draft) => {
                          if (!draft) return draft;
                          const assessments = draft.assessments.map((row, i) =>
                            i === index ? { ...row, evidence } : row,
                          );
                          return { ...draft, assessments };
                        })
                      }
                      placeholder={`Evidence for criterion ${index + 1}`}
                      accessibilityLabel={`Evidence for criterion ${index + 1}`}
                      multiline
                      className="min-h-[72px] h-auto py-2"
                      style={{ textAlignVertical: "top" }}
                    />
                  </View>
                );
              })}
              <TextField
                value={reviewDraft.feedback}
                editable={!busy && !draftStale}
                onChangeText={(feedback) =>
                  setReviewDraft((draft) =>
                    draft ? { ...draft, feedback } : draft,
                  )
                }
                placeholder="Reservations or correction instructions"
                multiline
                className="min-h-[72px] h-auto py-2"
                style={{ textAlignVertical: "top" }}
              />
              <View className="flex-row flex-wrap gap-2">
                <Button
                  size="sm"
                  disabled={busy || draftStale || !acceptReady}
                  onPress={() => decide("accepted")}
                >
                  <Text>Accept delivery</Text>
                </Button>
                <Button
                  size="sm"
                  variant="outline"
                  disabled={busy || draftStale}
                  onPress={() => decide("changes_requested")}
                >
                  <Text>Request corrections</Text>
                </Button>
                <Button
                  size="sm"
                  variant="ghost"
                  disabled={busy}
                  onPress={() => setReviewDraft(null)}
                >
                  <Text>Cancel</Text>
                </Button>
              </View>
            </View>
          ) : null}
        </>
      ) : null}

      {error ? (
        <Text className="text-xs text-destructive" accessibilityRole="alert">
          {error}
        </Text>
      ) : null}
    </View>
  );
}
