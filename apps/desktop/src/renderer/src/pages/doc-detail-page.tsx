import { useParams } from "react-router-dom";
import { DocDetail } from "@multica/views/docs";

export function DocDetailPage() {
  const { id } = useParams<{ id: string }>();

  if (!id) return null;
  return <DocDetail docId={id} />;
}
