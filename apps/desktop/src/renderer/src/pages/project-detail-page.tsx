import { useParams } from "react-router-dom";
import { ProjectDetail } from "@multica/views/projects/components";

export function ProjectDetailPage() {
  const { id } = useParams<{ id: string }>();

  if (!id) return null;
  return <ProjectDetail projectId={id} />;
}
