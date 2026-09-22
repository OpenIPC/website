type Timeout = {
  // Not NodeJS.Timeout: that name is only in scope with @types/node, and a
  // browser component library has no business demanding it of a consumer.
  current: ReturnType<typeof setTimeout> | undefined,
};
  
export function debounce<
  F extends (...args: Parameters<F>) => ReturnType<F>|void
>(
  fn: F,
  wait: number,
): [(...args: Parameters<F>) => ReturnType<F>|void, Timeout] {
  const timeout: Timeout = { current: undefined };

  function debounced(...args: Parameters<F>) {
    clearTimeout(timeout.current);
    timeout.current = setTimeout(() => fn(...args), wait);
  }

  return [debounced, timeout];
}

