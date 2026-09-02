import type { Metadata, Viewport } from "next";
import { DM_Mono, Inter } from "next/font/google";
import { SITE_DESCRIPTION, SITE_TITLE } from "@/lib/site";
import Toasts from "@/components/Toasts";
import Background from "@/components/Background";
import "./globals.css";

const inter = Inter({ subsets: ["latin"], variable: "--font-inter", display: "swap" });
const dmMono = DM_Mono({ subsets: ["latin"], weight: ["400", "500"], variable: "--font-mono", display: "swap" });

export const metadata: Metadata = {
  metadataBase: new URL("https://iris-tl.dev"),
  title: SITE_TITLE,
  description: SITE_DESCRIPTION,
  openGraph: {
    title: SITE_TITLE,
    description: SITE_DESCRIPTION,
    url: "https://iris-tl.dev",
    siteName: "iris",
    images: [{ url: "/og.png", width: 1200, height: 630, alt: "iris — transport layer for AI agents" }],
    type: "website",
  },
  twitter: { card: "summary_large_image", title: SITE_TITLE, description: SITE_DESCRIPTION, images: ["/og.png"] },
};

export const viewport: Viewport = {
  width: "device-width",
  initialScale: 1,
  themeColor: [
    { media: "(prefers-color-scheme: light)", color: "#FFFFFF" },
    { media: "(prefers-color-scheme: dark)", color: "#000a0f" },
  ],
};

// Resolve the theme before first paint: ?theme= override, stored preference, else the system setting.
const themeScript = `(function(){try{var q=new URLSearchParams(location.search).get('theme');var s=q||localStorage.getItem('iris-theme');var d=s?s==='dark':matchMedia('(prefers-color-scheme: dark)').matches;document.documentElement.setAttribute('data-theme',d?'dark':'light');}catch(e){}document.documentElement.classList.add('js');})();`;

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" suppressHydrationWarning className={`${inter.variable} ${dmMono.variable}`}>
      <head>
        <script dangerouslySetInnerHTML={{ __html: themeScript }} />
      </head>
      <body>
        <Background />
        <div className="page">{children}</div>
        <Toasts />
      </body>
    </html>
  );
}
