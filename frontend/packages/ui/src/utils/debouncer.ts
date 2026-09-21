type Timeout = {
  current: NodeJS.Timeout|undefined,
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

