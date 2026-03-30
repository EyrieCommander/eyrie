/** Open HTML content in a new tab for full-screen viewing / saving. */
export function openHtmlInNewTab(html: string) {
  const blob = new Blob([html], { type: "text/html" });
  const url = URL.createObjectURL(blob);
  window.open(url, "_blank");
  // 5s delay before revoking: gives the new tab time to load the blob URL
  setTimeout(() => URL.revokeObjectURL(url), 5000);
}
