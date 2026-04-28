/**
 * Format a timestamp string from the database into the org's timezone.
 * MySQL TIMESTAMP is stored as UTC — we append 'Z' if missing so JS parses it correctly.
 *
 * @param {string} dateStr - Date string from DB (e.g. "2026-04-01 12:34:56" or ISO)
 * @param {string} timezone - IANA timezone (e.g. "Asia/Kolkata"), defaults to browser local
 * @param {object} opts - Additional Intl.DateTimeFormat options
 * @returns {string} Formatted date string
 */
export function formatDateTime(dateStr, timezone, opts = {}) {
  if (!dateStr) return '—';
  // Ensure the string is treated as UTC
  const normalized = dateStr.endsWith('Z') ? dateStr : dateStr.replace(' ', 'T') + 'Z';
  const date = new Date(normalized);
  if (isNaN(date.getTime())) return dateStr;

  const options = {
    year: 'numeric', month: 'short', day: 'numeric',
    hour: '2-digit', minute: '2-digit',
    hour12: true,
    ...opts,
    ...(timezone ? { timeZone: timezone } : {})
  };
  return date.toLocaleString(undefined, options);
}

/**
 * Format date only (no time).
 */
export function formatDate(dateStr, timezone) {
  if (!dateStr) return '—';
  const normalized = dateStr.endsWith('Z') ? dateStr : dateStr.replace(' ', 'T') + 'Z';
  const date = new Date(normalized);
  if (isNaN(date.getTime())) return dateStr;

  const options = {
    year: 'numeric', month: 'short', day: 'numeric',
    ...(timezone ? { timeZone: timezone } : {})
  };
  return date.toLocaleDateString(undefined, options);
}

/**
 * Format time only.
 */
export function formatTime(dateStr, timezone) {
  if (!dateStr) return '—';
  const normalized = dateStr.endsWith('Z') ? dateStr : dateStr.replace(' ', 'T') + 'Z';
  const date = new Date(normalized);
  if (isNaN(date.getTime())) return dateStr;

  const options = {
    hour: '2-digit', minute: '2-digit', hour12: true,
    ...(timezone ? { timeZone: timezone } : {})
  };
  return date.toLocaleTimeString(undefined, options);
}
