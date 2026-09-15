export function browserTimezone() {
  return Intl.DateTimeFormat().resolvedOptions().timeZone
}
