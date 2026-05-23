import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  /* config options here */
};

export default nextConfig;

// Bind Cloudflare resources (R2/KV/D1) to `process.env` during `next dev`.
// Safe to leave in place — it's a no-op in production.
import { initOpenNextCloudflareForDev } from "@opennextjs/cloudflare";
initOpenNextCloudflareForDev();
