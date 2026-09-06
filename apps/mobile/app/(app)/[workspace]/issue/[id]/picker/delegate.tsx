/**
 * Delegate picker route for an existing issue (F01) — the assignee's partner.
 *
 * Same shape as ./assignee.tsx, and the same body component with
 * `showSquads={false}`: the server rejects delegate_type='squad' with a 400.
 * Naming a delegate starts no run, so there is no confirmation step and no
 * runtime requirement on the rows.
 *
 * The server also refuses a delegate equal to the assignee. That is left to
 * the 400 rather than filtered out of the list, so the two clients cannot
 * disagree about a rule only one of them enforces — mobile shows the same
 * rows web does.
 */
import { useLocalSearchParams, router } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import { AssigneePickerBody } from "@/components/issue/pickers/assignee-picker-body";
import { issueDetailOptions } from "@/data/queries/issues";
import { useUpdateIssue } from "@/data/mutations/issues";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useNativeSearchBar } from "@/lib/use-native-search-bar";

export default function IssueDelegatePickerRoute() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { data: issue } = useQuery(issueDetailOptions(wsId, id));
  const updateIssue = useUpdateIssue(id);
  const query = useNativeSearchBar("Search people", { autoFocus: true });

  const value =
    issue?.delegate_type && issue?.delegate_id
      ? { type: issue.delegate_type, id: issue.delegate_id }
      : null;

  return (
    <AssigneePickerBody
      value={value}
      query={query}
      showSquads={false}
      emptyLabel="No delegate"
      onChange={(next) => {
        // Both halves always travel together: the server validates the pair
        // and answers 400 when only one arrives.
        if (next === null) {
          updateIssue.mutate({ delegate_type: null, delegate_id: null });
        } else if (next.type !== "squad") {
          updateIssue.mutate({
            delegate_type: next.type,
            delegate_id: next.id,
          });
        }
        router.back();
      }}
    />
  );
}
