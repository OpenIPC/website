/** sitemap.xml, prerendered into the bundle (#303); see ../lib/sitemap.ts. */
import type { APIRoute } from 'astro';
import { sitemapXml } from '../lib/sitemap';

export const prerender = true;

export const GET: APIRoute = () =>
  new Response(sitemapXml(), { headers: { 'Content-Type': 'application/xml; charset=utf-8' } });
