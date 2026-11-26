// Cleanup selection and flow (milestone 8).
export function initClean() {}
export function cleanPanel(panel: HTMLElement) {
  panel.textContent = '';
}
export function addToSelection(_id?: number, _rule?: string) {}
export function isSelected(_id: number) {
  return false;
}
