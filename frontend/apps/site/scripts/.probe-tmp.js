() => {
  const out = [];
  const c = [...document.querySelectorAll('.container, .site-container')].find((e) => e.getBoundingClientRect().height > 300);
  const walk = (el, d) => {
    if (d > 2) return;
    for (const k of el.children) {
      const b = k.getBoundingClientRect();
      if (b.height < 1) continue;
      out.push('  '.repeat(d) + k.tagName.toLowerCase() + '.' + String(k.className).split(' ').slice(0,2).join('.')
        + ' y=' + Math.round(b.y + scrollY) + ' h=' + Math.round(b.height) + ' x=' + Math.round(b.x) + ' w=' + Math.round(b.width));
      walk(k, d + 1);
    }
  };
  walk(c, 0);
  return out.slice(0, 20).join('\n');
}
