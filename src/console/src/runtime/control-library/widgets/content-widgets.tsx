import type {
  LabelValueItem,
  LinkItem,
  WidgetComponentProps,
  WidgetRecord,
} from "./types";
import { WidgetRoot } from "./primitives";
import {
  recordsValue,
  sanitizeHtmlSubset,
  stringValue,
  valueToText,
  widgetClassName,
  widgetStyleVariables,
} from "./utils";

export type TextContentConfig = {
  body: string;
  visibility?: string;
};

export type MarkdownContentConfig = {
  markdown: string;
};

export type HtmlContentConfig = {
  html: string;
};

export type CalloutContentConfig = {
  body: string;
};

export type LinkListConfig = {
  links: readonly LinkItem[];
};

export type FaqItem = {
  question: string;
  answer: string;
};

export type FaqConfig = {
  items: readonly FaqItem[];
};

export type LabelValueListConfig = {
  items: readonly LabelValueItem[];
};

export const markdownLinesValue = (markdown: string): readonly string[] =>
  markdown.split("\n").filter((line) => line.trim().length > 0);

export const linkItemsFromRecords = (
  records: readonly WidgetRecord[],
): readonly LinkItem[] =>
  records.map((record) => ({
    label: stringValue(record.label, "Link"),
    href: stringValue(record.href, "#"),
    description: stringValue(record.description),
  }));

export const faqItemsFromRecords = (records: unknown): readonly FaqItem[] =>
  recordsValue(records).map((record) => ({
    question: stringValue(record.question, "Question"),
    answer: stringValue(record.answer),
  }));

export const labelValueItemsFromRecords = (
  records: readonly WidgetRecord[],
): readonly LabelValueItem[] =>
  records.map((record) => ({
    label: stringValue(record.label, "Value"),
    value: record.value,
    detail: stringValue(record.detail),
  }));

export function TextContentWidget({
  config,
  styleProps,
}: WidgetComponentProps<TextContentConfig>): JSX.Element {
  return (
    <WidgetRoot className="content-block" styleProps={styleProps}>
      <p>{config.body}</p>
      {config.visibility !== undefined ? <small>{config.visibility}</small> : null}
    </WidgetRoot>
  );
}

export function MarkdownContentWidget({
  config,
  styleProps,
}: WidgetComponentProps<MarkdownContentConfig>): JSX.Element {
  return (
    <WidgetRoot className="content-block" styleProps={styleProps}>
      {markdownLinesValue(config.markdown).map((line) => (
        <p key={line}>{line}</p>
      ))}
    </WidgetRoot>
  );
}

export function HtmlContentWidget({
  config,
  styleProps,
}: WidgetComponentProps<HtmlContentConfig>): JSX.Element {
  return (
    <WidgetRoot className="content-block" styleProps={styleProps}>
      <div
        dangerouslySetInnerHTML={{
          __html: sanitizeHtmlSubset(config.html),
        }}
      />
    </WidgetRoot>
  );
}

export function CalloutContentWidget({
  config,
  styleProps,
}: WidgetComponentProps<CalloutContentConfig>): JSX.Element {
  return (
    <WidgetRoot className="callout-body" styleProps={styleProps}>
      <p>{config.body}</p>
    </WidgetRoot>
  );
}

export function LinkListWidget({
  config,
  styleProps,
}: WidgetComponentProps<LinkListConfig>): JSX.Element {
  return (
    <ul
      className={widgetClassName("link-list", styleProps)}
      style={widgetStyleVariables(styleProps)}
    >
      {config.links.map((link) => (
        <li key={`${link.label}-${link.href}`}>
          <a href={link.href}>{link.label}</a>
          {link.description !== undefined ? <small>{link.description}</small> : null}
        </li>
      ))}
    </ul>
  );
}

export function FaqWidget({
  config,
  styleProps,
}: WidgetComponentProps<FaqConfig>): JSX.Element {
  return (
    <WidgetRoot className="faq-list" styleProps={styleProps}>
      {config.items.map((item) => (
        <details key={item.question}>
          <summary>{item.question}</summary>
          <p>{item.answer}</p>
        </details>
      ))}
    </WidgetRoot>
  );
}

export function LabelValueListWidget({
  config,
  styleProps,
}: WidgetComponentProps<LabelValueListConfig>): JSX.Element {
  return (
    <dl
      className={widgetClassName("label-value-list", styleProps)}
      style={widgetStyleVariables(styleProps)}
    >
      {config.items.map((item) => (
        <div key={item.label}>
          <dt>{item.label}</dt>
          <dd>{valueToText(item.value)}</dd>
          {item.detail !== undefined ? <small>{item.detail}</small> : null}
        </div>
      ))}
    </dl>
  );
}
