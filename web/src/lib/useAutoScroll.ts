import { useEffect, useRef, type RefObject } from "react";

// WHY threshold instead of exact match: scrollHeight - scrollTop - clientHeight
// is often off by 1-2px due to fractional rendering. A 50px threshold means
// "close enough to the bottom" — the user hasn't intentionally scrolled up.
const NEAR_BOTTOM_THRESHOLD = 50;

/**
 * Auto-scrolls a container to the bottom when dependencies change,
 * but only if the user hasn't scrolled up. Returns a ref to attach
 * to the scrollable container.
 */
export function useAutoScroll(deps: unknown[]): RefObject<HTMLDivElement | null> {
  const ref = useRef<HTMLDivElement | null>(null);
  const isNearBottom = useRef(true);

  // Track scroll position to detect if user scrolled up
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const handleScroll = () => {
      const { scrollTop, scrollHeight, clientHeight } = el;
      isNearBottom.current = scrollHeight - scrollTop - clientHeight < NEAR_BOTTOM_THRESHOLD;
    };
    el.addEventListener("scroll", handleScroll, { passive: true });
    return () => el.removeEventListener("scroll", handleScroll);
  }, []);

  // Scroll to bottom only if user is near the bottom
  useEffect(() => {
    if (isNearBottom.current) {
      ref.current?.scrollTo(0, ref.current.scrollHeight);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps);

  return ref;
}
