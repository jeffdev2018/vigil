"use client";

import { use } from "react";
import { CycleDetail } from "@multica/views/cycles/components";

export default function CycleDetailPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  return <CycleDetail cycleId={id} />;
}
