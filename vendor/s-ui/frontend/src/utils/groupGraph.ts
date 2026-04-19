/**
 * Groups with ACL containment edges: each row lists container group ids
 * (`member_of_group_ids` / `parent_group_ids`) — the group is a member of those parents.
 * Traversal: from rootId follow child links (groups that list root as a parent).
 */
export function descendantGroupIds(
  rootId: number,
  groups: { id: number; parent_group_ids?: number[]; member_of_group_ids?: number[] }[],
): number[] {
  const byParent = new Map<number, number[]>()
  for (const g of groups) {
    const parents = g.member_of_group_ids ?? g.parent_group_ids ?? []
    for (const p of parents) {
      if (!byParent.has(p)) byParent.set(p, [])
      byParent.get(p)!.push(g.id)
    }
  }
  const out: number[] = []
  const queue: number[] = [rootId]
  const seen = new Set<number>()
  while (queue.length > 0) {
    const cur = queue.shift()!
    if (seen.has(cur)) continue
    seen.add(cur)
    out.push(cur)
    for (const id of byParent.get(cur) ?? []) {
      queue.push(id)
    }
  }
  return out
}
