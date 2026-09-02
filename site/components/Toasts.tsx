"use client";

import { useEffect, useState } from "react";
import { Toaster } from "sonner";

/** Sonner toaster that follows the page's data-theme. */
export default function Toasts() {
  const [theme, setTheme] = useState<"light" | "dark">("light");
  useEffect(() => {
    const read = () => setTheme(document.documentElement.getAttribute("data-theme") === "dark" ? "dark" : "light");
    read();
    const mo = new MutationObserver(read);
    mo.observe(document.documentElement, { attributes: true, attributeFilter: ["data-theme"] });
    return () => mo.disconnect();
  }, []);
  return (
    <Toaster
      theme={theme}
      position="bottom-right"
      duration={1800}
      gap={8}
      offset={24}
      toastOptions={{ unstyled: true, className: "toast" }}
    />
  );
}
