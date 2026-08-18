// 列表滚动位置保存（借鉴 dash-v2 saveMainPageScrollPosition）：
// 离开页面时记录 window 滚动位置，返回时恢复（按路径区分）。
import { useEffect } from "react";

const scrollPositions = new Map<string, number>();

/** 在页面挂载时恢复、卸载时保存滚动位置。 */
export function useScrollRestore(key?: string): void {
  useEffect(() => {
    const k = key ?? location.pathname;
    const saved = scrollPositions.get(k);
    if (saved !== undefined) {
      requestAnimationFrame(() => window.scrollTo(0, saved));
    }
    const onScroll = () => scrollPositions.set(k, window.scrollY);
    window.addEventListener("scroll", onScroll, { passive: true });
    return () => {
      window.removeEventListener("scroll", onScroll);
      scrollPositions.set(k, window.scrollY);
    };
  }, [key]);
}
