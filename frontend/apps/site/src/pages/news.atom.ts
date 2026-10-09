/** /news.atom, prerendered into the bundle (#212); see ../lib/news-feed.ts. */
import type { APIRoute } from 'astro';
import { atomXml } from '../lib/news-feed';

export const prerender = true;

export const GET: APIRoute = () =>
  new Response(atomXml(), { headers: { 'Content-Type': 'application/atom+xml; charset=utf-8' } });
