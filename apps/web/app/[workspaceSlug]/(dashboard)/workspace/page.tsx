import { redirect } from "next/navigation";

// The assets page has one address per view; this one is the entry and lands on
// the wiki, so the address bar always names what is on screen.
export default async function Page({
  params,
}: {
  params: Promise<{ workspaceSlug: string }>;
}) {
  const { workspaceSlug } = await params;
  redirect(`/${workspaceSlug}/workspace/wiki`);
}
