import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  output: process.env.GOVISUALIZE_STANDALONE === "1" ? "standalone" : undefined,
};

export default nextConfig;
