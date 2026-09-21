export const ESRI_DARK_GRAY =
  'https://server.arcgisonline.com/ArcGIS/rest/services/Canvas/World_Dark_Gray_Base/MapServer/tile/{z}/{y}/{x}';
export const ESRI_LIGHT_GRAY =
  'https://server.arcgisonline.com/ArcGIS/rest/services/Canvas/World_Light_Gray_Base/MapServer/tile/{z}/{y}/{x}';

export function tileURL(theme: 'dark' | 'light'): string {
  return theme === 'light' ? ESRI_LIGHT_GRAY : ESRI_DARK_GRAY;
}

export function shouldUpdateLatLng(frozen: boolean): boolean {
  return !frozen;
}
