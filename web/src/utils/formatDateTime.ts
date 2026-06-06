/**
 * Formats an ISO 8601 date string to "YYYY-MM-DD HH:mm:ss" format.
 * Returns '-' for invalid or missing input.
 */
export function formatDateTime(isoString: string | undefined): string {
  if (!isoString) return '-';
  const d = new Date(isoString);
  if (isNaN(d.getTime())) return '-';
  const pad = (n: number) => n.toString().padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
}
