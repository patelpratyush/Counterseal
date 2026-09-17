import type { NextConfig } from "next";
const nextConfig: NextConfig = {
  output: "standalone",
  distDir: process.env.HANDOFFGUARD_LOCAL_PREVIEW === "1" ? ".next-preview" : ".next",
  poweredByHeader: false,
  async headers() {
    return [
      {
        source: "/:path*",
        headers: [
          { key: "X-Content-Type-Options", value: "nosniff" },
          { key: "X-Frame-Options", value: "DENY" },
          { key: "Referrer-Policy", value: "no-referrer" },
        ],
      },
    ];
  },
};
export default nextConfig;
