import { useEffect, useRef, type RefObject } from "react";

/**
 * Auto-scrolls a container to the bottom when dependencies change.
 * Returns a ref to attach to the scrollable container.
 */
export function useAutoScroll(deps: unknown[]): RefObject<HTMLDivElement | null> {
  const ref = useRef<HTMLDivElement | null>(null);
  useEffect(() => {
    ref.current?.scrollTo(0, ref.current.scrollHeight);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps);
  return ref;
}
