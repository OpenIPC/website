/**
 * /ru/news.atom and /zh/news.atom, prerendered with the pages (#212).
 * English lives at /news.atom; see ../news.atom.ts and ../../lib/news-feed.ts.
 */
import type { APIRoute, GetStaticPaths } from 'astro';
import { atomXml } from '../../lib/news-feed';
import { LOCALES, type Locale } from '../../lib/i18n';
import { POSTS } from '../../lib/news';

export const prerender = true;

export const getStaticPaths: GetStaticPaths = () =>
  LOCALES.filter((locale) => locale !== 'en').map((locale) => ({ params: { locale } }));

export const GET: APIRoute = ({ params }) =>
  new Response(atomXml(POSTS, params.locale as Locale), {
    headers: { 'Content-Type': 'application/atom+xml; charset=utf-8' },
  });
