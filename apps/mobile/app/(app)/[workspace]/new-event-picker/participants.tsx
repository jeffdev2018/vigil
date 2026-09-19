/**
 * Participants picker route for the in-progress New Event draft. Mirrors
 * `new-issue-picker/assignee.tsx`: native iOS Stack header + UISearchController
 * (registered in ../_layout.tsx), search wiring via `useNativeSearchBar`.
 *
 * Multi-select: unlike the issue assignee picker this does NOT `router.back()`
 * on a single tap — the user keeps adding/removing participants and taps the
 * native back chevron / swipe-to-dismiss when done.
 */
import { ParticipantPickerBody } from "@/components/calendar/participant-picker-body";
import { useNewEventDraftStore } from "@/data/stores/new-event-draft-store";
import { useNativeSearchBar } from "@/lib/use-native-search-bar";

export default function NewEventParticipantsPickerRoute() {
  const participants = useNewEventDraftStore((s) => s.participants);
  const setParticipants = useNewEventDraftStore((s) => s.setParticipants);
  const query = useNativeSearchBar("Search people and agents", { autoFocus: false });

  return (
    <ParticipantPickerBody
      value={participants}
      query={query}
      onChange={setParticipants}
    />
  );
}
