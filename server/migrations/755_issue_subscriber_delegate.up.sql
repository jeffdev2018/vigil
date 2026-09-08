-- F01: a human delegate watches the issue they were named on, with the same
-- delivery tier as an assignee.
--
-- The value is 'delegate', NOT the existing 'delegated'. They are different
-- facts and must not be conflated:
--
--   * 'delegated' (249, MUL-5483) means "your agent filed this on your
--     behalf". It carries a deliberately NARROWED delivery tier
--     (deliverToSubscriber / delegatedAlwaysNotifTypes), and
--     AddIssueSubscriber treats it as the one reason an active row may be
--     upgraded away from when the user becomes directly involved.
--   * 'delegate' (this one) means "you were named as the assignee's partner
--     on this issue" — a direct involvement, like 'assignee'. It must get the
--     full tier, and it must not be silently overwritten.
--
-- Reusing 'delegated' would have suppressed the delegate_assigned inbox item
-- and let the first comment rewrite the row's reason.
--
-- Only a WIDENING, so every existing row already satisfies it; 756 validates.
ALTER TABLE issue_subscriber DROP CONSTRAINT IF EXISTS issue_subscriber_reason_check;
ALTER TABLE issue_subscriber ADD CONSTRAINT issue_subscriber_reason_check
    CHECK (reason IN ('creator', 'assignee', 'commenter', 'mentioned', 'manual', 'autopilot', 'delegated', 'delegate'))
    NOT VALID;
