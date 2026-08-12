"use client";

import { use } from "react";
import { DocDetail } from "@multica/views/docs";

export default function DocDetailPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = use(params);
  return <DocDetail docId={id} />;
}
