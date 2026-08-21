import { redirect } from "next/navigation";

// Kept so links and bookmarks written before the assets page had one address
// per view still land somewhere.
export default async function Page({
  params,
}: {
  params: Promise<{ workspaceSlug: string }>;
}) {
  const { workspaceSlug } = await params;
  redirect(`/${workspaceSlug}/workspace/wiki`);
}
