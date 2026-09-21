export const CAR_BODY =
  'M6 .5C9 .5 10.5 1.6 10.8 3.6L11.3 8V19C11.3 20.9 9.8 21.5 6 21.5C2.2 21.5 .7 20.9 .7 19V8L1.2 3.6C1.5 1.6 3 .5 6 .5Z';
export const CAR_GLASS = 'M2.2 6.2h7.6L9 10H3z M2.6 15.5h6.8v2.5H2.6z';

export function escapeHtml(value: string): string {
  return value
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;');
}

export function escapeAttr(value: string): string {
  return escapeHtml(value);
}
