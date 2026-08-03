import { useParams } from "react-router-dom";
import { IssueDetailRoute } from "@multica/views/issues/components";

export function IssueDetailPage({ onDelete }: { onDelete?: () => void }) {
  const { id } = useParams<{ id: string }>();

  if (!id) return null;
  // Render errors bubble to the root route errorElement (DesktopRouteErrorPage),
  // which contains the crash inside the tab content pane. No page-level boundary
  // here — a whole-page wrapper duplicates the route-level error UI.
  return <IssueDetailRoute routeId={id} onDelete={onDelete} />;
}
