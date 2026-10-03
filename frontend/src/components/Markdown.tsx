import type { ReactNode } from "react";

/** A small renderer for release notes: headings, bullet lists, paragraphs, and
    **bold**, `code` and links inside them. It builds elements, never HTML, so
    whatever a release says cannot inject markup. Anything else shows as text. */

function inline(text: string): ReactNode[] {
  const out: ReactNode[] = [];
  const re = /(\*\*[^*]+\*\*|`[^`]+`|\[[^\]]+\]\(https:\/\/[^)\s]+\))/g;
  let last = 0;
  let key = 0;
  for (const m of text.matchAll(re)) {
    if (m.index > last) out.push(text.slice(last, m.index));
    const tok = m[0];
    if (tok.startsWith("**")) out.push(<strong key={key++}>{tok.slice(2, -2)}</strong>);
    else if (tok.startsWith("`")) out.push(<code key={key++}>{tok.slice(1, -1)}</code>);
    else {
      const [, label, href] = /^\[([^\]]+)\]\((.+)\)$/.exec(tok) ?? [];
      out.push(
        <a key={key++} href={href} target="_blank" rel="noopener noreferrer">
          {label}
        </a>,
      );
    }
    last = m.index + tok.length;
  }
  if (last < text.length) out.push(text.slice(last));
  return out;
}

export default function Markdown({ text }: { text: string }) {
  const blocks: ReactNode[] = [];
  let list: string[] = [];
  let para: string[] = [];
  let key = 0;
  const flushList = () => {
    if (!list.length) return;
    blocks.push(
      <ul key={key++}>
        {list.map((item, i) => (
          <li key={i}>{inline(item)}</li>
        ))}
      </ul>,
    );
    list = [];
  };
  const flushPara = () => {
    if (!para.length) return;
    blocks.push(<p key={key++}>{inline(para.join(" "))}</p>);
    para = [];
  };
  for (const raw of text.replace(/\r/g, "").split("\n")) {
    const line = raw.trimEnd();
    const heading = /^#{1,6}\s+(.*)$/.exec(line);
    const bullet = /^\s*[-*]\s+(.*)$/.exec(line);
    if (heading) {
      flushList();
      flushPara();
      blocks.push(<h4 key={key++}>{inline(heading[1])}</h4>);
    } else if (bullet) {
      flushPara();
      list.push(bullet[1]);
    } else if (/^\s+\S/.test(line) && list.length) {
      list[list.length - 1] += " " + line.trim(); // a wrapped bullet
    } else if (line.trim() === "") {
      flushList();
      flushPara();
    } else {
      flushList();
      para.push(line.trim());
    }
  }
  flushList();
  flushPara();
  return <div className="md">{blocks}</div>;
}
