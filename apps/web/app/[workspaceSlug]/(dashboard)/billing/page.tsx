"use client";

import { useFeatureEnabled } from "@multica/core/config";
import { BILLING_WORKSPACE_SUBSCRIPTIONS_FLAG } from "@multica/core/feature-flags";
import { BillingTestPage } from "@multica/views/billing";

// Account-level test page for the cloud-billing API surface. Despite
// living under [workspaceSlug] — that's where the dashboard layout
// requires every page to sit — none of the data here is workspace-
// scoped. The slug just keeps the route inside the authenticated
// shell.
//
// BillingTestPage is upstream's own scaffold, deleted when the real UI
// ships, so the guard lives here rather than inside it: one additive
// file that survives a sync instead of an edit that conflicts on every
// one. Gated on the same flag as the real Settings → Billing tab
// (`BillingTab`), which is the app's only signal that cloud billing is
// live on this deployment. Fail-closed by default, so on a self-hosted
// instance with no cloud configured the route renders nothing instead
// of showing every member a scaffold whose every request 503s.
export default function BillingRoute() {
  const billingEnabled = useFeatureEnabled(
    BILLING_WORKSPACE_SUBSCRIPTIONS_FLAG,
    false,
  );
  return billingEnabled ? <BillingTestPage /> : null;
}
