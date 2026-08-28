// Render unix timestamps in the visitor's locale and timezone.
export default function initTimestamps() {
  document.querySelectorAll('span[data-timestamp]').forEach(el => {
    const ts = el.dataset['timestamp']
    const date = new Date(ts * 1000)
    el.textContent = date.toLocaleString([], { dateStyle: 'medium', timeStyle: 'short' })
  })
}
