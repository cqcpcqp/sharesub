export const buildInfo = Object.freeze({
  version: __SHARESUB_VERSION__,
  revision: __SHARESUB_REVISION__,
})

export function shortRevision(revision: string): string {
  return revision.slice(0, 12)
}
