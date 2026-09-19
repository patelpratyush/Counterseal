import type { Metadata } from "next";
import localFont from "next/font/local";
import { Theme } from "@/components/theme";
import "./globals.css";
const sans = localFont({
  src: "../../node_modules/@fontsource-variable/manrope/files/manrope-latin-wght-normal.woff2",
  variable: "--font-manrope",
  display: "swap",
});
const mono = localFont({
  src: "../../node_modules/@fontsource/ibm-plex-mono/files/ibm-plex-mono-latin-400-normal.woff2",
  variable: "--font-plex",
  display: "swap",
});
export const metadata: Metadata = {
  title: "Counterseal — Authority console",
  description:
    "Inspect signed agent handoffs, inherited permissions, and authorization decisions.",
  robots: { index: false, follow: false },
};
export default function Layout({ children }: { children: React.ReactNode }) {
  return (
    <html
      lang="en"
      suppressHydrationWarning
      className={`${sans.variable} ${mono.variable}`}
    >
      <body>
        <Theme>{children}</Theme>
      </body>
    </html>
  );
}
