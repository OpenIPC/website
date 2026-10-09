#!/usr/bin/env python3
"""Translate a news post in data/news into another language with an LLM.

    tools/news/translate.py zh data/news/2026-10-09-slug.md

writes data/news/2026-10-09-slug.zh.md beside it, and `--check` verifies an
existing translation against its original without calling anything.

A release note is mostly prose, but the parts that are not -- a config key, a
measurement, a link -- are the parts a reader acts on, and a model that
paraphrases one has broken the post rather than translated it. So the prompt
names them, and `--check` proves it afterwards: same links, same code spans,
same list items, same heading levels, no raw HTML.

The model returns JSON rather than a file. The front matter is assembled here
from it, so a mistranslated quotation mark cannot break the YAML, and `date`
is copied from the original rather than trusted to a language model.

Configuration is in the environment, because an endpoint and a key do not
belong in a public repository:

    NEWS_LLM_BASE_URL   an OpenAI-compatible base, e.g. https://host/api
    NEWS_LLM_API_KEY    bearer token
    NEWS_LLM_MODEL      model name as that endpoint spells it
    NEWS_LLM_CA_BUNDLE  optional: a CA file, when the endpoint is internal
    NEWS_LLM_INSECURE   optional: 1 to skip TLS verification entirely

INSECURE sends the key over a connection nobody has authenticated. It exists
because some internal endpoints present a certificate that cannot be verified
at all, and it is off unless asked for, so it is never the accident.
"""
import argparse, json, os, re, ssl, sys, urllib.request

LANGUAGES = {'ru': 'Russian', 'zh': 'Simplified Chinese (zh-CN)'}

SYSTEM = """You translate release notes for OpenIPC, an open-source firmware for IP cameras, from English into {language} for openipc.org.

WHO READS THIS
Camera installers, integrators and hobbyists who flash their own devices. They know the hardware and the jargon. Write the way an engineer writes for other engineers: direct, concrete, no marketing register, no padding. Short sentences. Do not add politeness or filler the English does not have, and do not explain what the English leaves unexplained.

TRANSLATE
- All prose, headings and link text.
- The title and the summary.

NEVER TRANSLATE, NEVER REWRITE — copy these through byte for byte
- Anything inside `backticks`: config keys, commands, endpoints, file names.
- URLs and link targets, including site-relative ones such as /club. Keep <https://...> autolinks exactly as they are, on their own line.
- Numbers, units and measurements: 2.06 W, 46-50 ms, 640x384, 758 kbit/s, 70-86%, -27%.
- Chip, company, product and protocol names in Latin script: OpenIPC, majestic, ipctool, Coupler, HiSilicon, Goke, SigmaStar, Ingenic, XiongMai, Frigate, MediaMTX, YOLOv8, IMX335, RTSP, ONVIF, WebRTC, WHIP, MSE, MJPEG, HLS, UVC, U-Boot, RTTTL, DNG, NPU.

KEEP THE SHAPE
- Markdown in, Markdown out: the same list items in the same order, the same heading levels, the same paragraph breaks, the same bold markers.
- Never output raw HTML. The site refuses a post containing any, and the build fails.
- Do not add, drop, merge or reorder anything. One English item is one translated item.

{glossary}
OUTPUT
A single JSON object and nothing else — no code fence, no commentary:
{{"title": "...", "summary": "...", "author": "...", "body": "..."}}
`body` is the whole Markdown body after the front matter, newlines as \\n. `author` is the English author line in the target language."""

# One wording per term, so five posts do not drift apart from each other.
GLOSSARY = {
    'zh': """TERMS — use these consistently
firmware 固件 · camera 摄像机 · sensor 传感器 · board 板卡 · stream 码流 · bitrate 码率 · exposure 曝光 · shutter 快门 · frame 帧 · keyframe 关键帧 · gain 增益 · IR-cut filter 红外滤光片 · IR lamp 红外补光灯 · pan/tilt head 云台 · autofocus 自动对焦 · motorised lens 电动镜头 · web interface 网页界面 · recording 录像 · SD card SD 卡 · flash dump 闪存转储 · boot log 启动日志 · crash 崩溃 · detection 检测 · catalogue 目录 · build 构建版本 · nightly build 每夜构建
""",
    'ru': """TERMS — use these consistently
firmware прошивка · camera камера · sensor сенсор (never "датчик": that is a light sensor, a different part) · board плата · stream поток · bitrate битрейт · exposure выдержка · shutter затвор · frame кадр · keyframe опорный кадр · gain усиление · IR-cut filter инфракрасный фильтр · pan/tilt head поворотная головка · web interface веб-интерфейс · recording запись · flash dump дамп флеша · boot log загрузочный лог · maintainers команда проекта · colour chart цветовая шкала (never "мишень")
""",
}


def front_matter(text):
    m = re.match(r'^---\r?\n(.*?)\r?\n---\r?\n(.*)$', text, re.S)
    if not m:
        sys.exit('no front matter: the file must open with a --- block')
    meta = {}
    for line in m.group(1).split('\n'):
        k, _, v = line.partition(':')
        v = v.strip()
        if len(v) >= 2 and v[0] == v[-1] and v[0] in '"\'':
            v = v[1:-1].replace('\\"', '"').replace('\\\\', '\\')
        meta[k.strip()] = v
    return meta, m.group(2)


def shape(path):
    """What a translation has to keep identical to its original."""
    body = re.sub(r'^---\r?\n.*?\r?\n---\r?\n', '', open(path, encoding='utf-8').read(), flags=re.S)
    return {
        'links': sorted(re.findall(r'<(https?://[^>]+)>', body)
                        + re.findall(r'\]\((/[^)]+|https?://[^)]+)\)', body)),
        'code': sorted(re.findall(r'`([^`]+)`', body)),
        'items': len(re.findall(r'^- ', body, re.M)),
        'headings': re.findall(r'^(#+) ', body, re.M),
        'html': re.findall(r'<(?!https?://|/?[a-z]+@)[a-zA-Z/][^>]*>', body),
    }


def check(src, dst):
    a, b = shape(src), shape(dst)
    bad = []
    for field in ('links', 'code'):
        missing, extra = set(a[field]) - set(b[field]), set(b[field]) - set(a[field])
        if missing or extra:
            bad.append(f'{field}: missing {sorted(missing)}, unexpected {sorted(extra)}')
    if a['items'] != b['items']:
        bad.append(f"{a['items']} list items became {b['items']}")
    if a['headings'] != b['headings']:
        bad.append(f"headings {a['headings']} became {b['headings']}")
    if b['html']:
        bad.append(f"raw HTML, which the site refuses: {b['html'][:3]}")
    return bad


def env(name, required=True):
    value = os.environ.get(name, '').strip()
    if required and not value:
        sys.exit(f'{name} is not set. See the docstring; put it in ~/.zshlocal.')
    return value


def translate(path, lang):
    meta, body = front_matter(open(path, encoding='utf-8').read())
    system = SYSTEM.format(language=LANGUAGES[lang], glossary=GLOSSARY.get(lang, ''))
    user = (f"Translate this post into {LANGUAGES[lang]}.\n\ntitle: {meta['title']}\n"
            f"summary: {meta['summary']}\nauthor: {meta.get('author', 'OpenIPC team')}\n\nbody:\n{body}")

    if env('NEWS_LLM_INSECURE', required=False) == '1':
        ctx = ssl.create_default_context()
        ctx.check_hostname, ctx.verify_mode = False, ssl.CERT_NONE
        print('warning: TLS verification is off; the key goes over an unauthenticated '
              'connection', file=sys.stderr)
    else:
        ctx = ssl.create_default_context(cafile=env('NEWS_LLM_CA_BUNDLE', required=False) or None)

    req = urllib.request.Request(
        env('NEWS_LLM_BASE_URL').rstrip('/') + '/chat/completions', method='POST',
        data=json.dumps({'model': env('NEWS_LLM_MODEL'),
                         'messages': [{'role': 'system', 'content': system},
                                      {'role': 'user', 'content': user}],
                         'temperature': 0.2, 'max_tokens': 16000}).encode(),
        headers={'Authorization': 'Bearer ' + env('NEWS_LLM_API_KEY'),
                 'Content-Type': 'application/json'})
    with urllib.request.urlopen(req, timeout=900, context=ctx) as r:
        reply = json.load(r)['choices'][0]['message']['content']

    out = json.loads(re.sub(r'^\s*```(?:json)?\s*|\s*```\s*$', '', reply.strip()))
    q = lambda s: '"' + s.replace('\\', '\\\\').replace('"', '\\"') + '"'
    return (f"---\ntitle: {q(out['title'])}\ndate: {meta['date']}\n"
            f"summary: {q(out['summary'])}\nauthor: {q(out['author'])}\n---\n\n"
            + out['body'].strip() + "\n")


def main():
    p = argparse.ArgumentParser(description=__doc__.split('\n')[0])
    p.add_argument('language', choices=sorted(LANGUAGES))
    p.add_argument('post', help='the English post in data/news')
    p.add_argument('--check', action='store_true',
                   help='verify the existing translation instead of making one')
    a = p.parse_args()

    if a.post.endswith(('.ru.md', '.zh.md')):
        sys.exit('give the English post; the translation is what this writes')
    dst = a.post[:-3] + f'.{a.language}.md'

    if not a.check:
        open(dst, 'w', encoding='utf-8').write(translate(a.post, a.language))
        print('wrote', dst)

    if not os.path.exists(dst):
        sys.exit(f'{dst} does not exist')
    bad = check(a.post, dst)
    for line in bad:
        print(f'{dst}: {line}', file=sys.stderr)
    if bad:
        sys.exit(f'{dst} does not match its original; fix it by hand before committing')
    print(f'{dst}: links, code, items and headings all match the original')


if __name__ == '__main__':
    main()
