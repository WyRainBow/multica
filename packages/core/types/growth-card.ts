/**
 * A growth card is what one delivery taught the person who delivered it.
 *
 * Eight named fields rather than one prose body: the questions are the method.
 * Free text lets a writer skip exactly the ones that would expose a delivery
 * they never understood — "我亲自验证了什么" is empty precisely when the
 * verification did not happen, and that blank is the point.
 *
 * Kept separately from the issue it came out of because it has to outlive it:
 * a requirement gets closed, what it taught does not stop being true.
 */
export interface GrowthCard {
  id: string;
  workspace_id: string;
  /** The requirement this came out of. Null is normal — not every delivery
   *  was tracked as an issue. */
  issue_id: string | null;
  author_type: string;
  author_id: string;
  /** 需求 — also the card's display name. */
  title: string;
  /** 系统涉及 */
  systems: string;
  /** 我原本不会的东西 */
  unknowns: string;
  /** Agent 给出的方案 */
  agent_plan: string;
  /** 我确认理解的关键点 */
  understood: string;
  /** 我亲自验证了什么 */
  verified: string;
  /** 这次真正学会了什么 */
  learned: string;
  /** 下次要补的知识 */
  next_gaps: string;
  created_at: string;
  updated_at: string;
}

/**
 * The eight writable fields, all optional — a card is filled in over several
 * sittings and is worth saving half-written.
 */
export interface GrowthCardFields {
  title?: string;
  systems?: string;
  unknowns?: string;
  agent_plan?: string;
  understood?: string;
  verified?: string;
  learned?: string;
  next_gaps?: string;
}

export interface CreateGrowthCardRequest extends GrowthCardFields {
  issue_id?: string | null;
}

export interface UpdateGrowthCardRequest extends GrowthCardFields {
  /** Explicit null detaches the card from its requirement; omitting the field
   *  leaves the link alone. */
  issue_id?: string | null;
}

export interface GrowthCardListResponse {
  cards: GrowthCard[];
  total: number;
}
