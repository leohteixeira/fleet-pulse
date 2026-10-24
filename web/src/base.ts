// Router paths stay relative to the router; network requests use the build base.
export function apiURL(path: string): string {
  return `${import.meta.env.BASE_URL.replace(/\/$/, '')}${path}`;
}
