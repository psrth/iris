"use client";

import { useEffect, useState } from "react";
import { GrainGradient } from "@paper-design/shaders-react";

const DARK = { colors: ["#242424"], colorBack: "#000a0f" };
const LIGHT = { colors: ["#e9e9e9"], colorBack: "#ffffff" };

/** Full-page grain gradient behind the content; follows data-theme. */
export default function Background() {
  const [dark, setDark] = useState(false);
  useEffect(() => {
    const read = () => setDark(document.documentElement.getAttribute("data-theme") === "dark");
    read();
    const mo = new MutationObserver(read);
    mo.observe(document.documentElement, { attributes: true, attributeFilter: ["data-theme"] });
    return () => mo.disconnect();
  }, []);
  const c = dark ? DARK : LIGHT;
  return (
    <div className="bgShader" aria-hidden="true">
      <GrainGradient
        width="100%"
        height="100%"
        colors={c.colors}
        colorBack={c.colorBack}
        softness={0.7}
        intensity={0.15}
        noise={0.5}
        shape="wave"
        speed={1}
      />
    </div>
  );
}
