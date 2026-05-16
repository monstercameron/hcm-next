import { useState } from "react";
import type { WidgetComponentProps } from "./types";
import { WidgetRoot } from "./primitives";

export type ImageMediaConfig = {
  src: string;
  alt: string;
};

export type AudioMediaConfig = {
  src?: string;
  transcript?: string;
};

export type VideoMediaConfig = {
  src?: string;
  transcript?: string;
};

export type PdfViewerConfig = {
  src?: string;
  title: string;
  label: string;
  pageCount: number;
  minZoom?: number;
  maxZoom?: number;
  zoomStep?: number;
};

export type DocumentPreviewConfig = PdfViewerConfig & {
  documentType?: string;
  status?: string;
};

export type MediaConfig =
  | ({ mediaType: "image" } & ImageMediaConfig)
  | ({ mediaType: "audio" } & AudioMediaConfig)
  | ({ mediaType: "video" } & VideoMediaConfig)
  | ({ mediaType: "pdf" } & PdfViewerConfig);

export function ImageMediaWidget({
  config,
  styleProps,
}: WidgetComponentProps<ImageMediaConfig>): JSX.Element {
  return (
    <WidgetRoot className="media-widget" styleProps={styleProps}>
      <figure className="media-image-frame">
        <img
          alt={config.alt}
          className="media-image"
          loading="eager"
          onError={(event) => {
            event.currentTarget.hidden = true;
          }}
          src={config.src}
        />
        <figcaption>{config.alt}</figcaption>
      </figure>
    </WidgetRoot>
  );
}

export function AudioMediaWidget({
  config,
  styleProps,
}: WidgetComponentProps<AudioMediaConfig>): JSX.Element {
  return (
    <WidgetRoot className="media-widget" styleProps={styleProps}>
      {config.src !== undefined && config.src.length > 0 ? (
        <audio controls src={config.src}>
          {config.transcript}
        </audio>
      ) : (
        <p className="placeholder-text">{config.transcript}</p>
      )}
    </WidgetRoot>
  );
}

export function VideoMediaWidget({
  config,
  styleProps,
}: WidgetComponentProps<VideoMediaConfig>): JSX.Element {
  return (
    <WidgetRoot className="media-widget" styleProps={styleProps}>
      {config.src !== undefined && config.src.length > 0 ? (
        <video controls src={config.src}>
          {config.transcript}
        </video>
      ) : (
        <p className="placeholder-text">{config.transcript}</p>
      )}
    </WidgetRoot>
  );
}

export function PdfViewerWidget({
  config,
  styleProps,
}: WidgetComponentProps<PdfViewerConfig>): JSX.Element {
  const pageCount = Math.max(1, config.pageCount);
  const minZoom = config.minZoom ?? 75;
  const maxZoom = config.maxZoom ?? 150;
  const zoomStep = config.zoomStep ?? 25;
  const [page, setPage] = useState(1);
  const [zoom, setZoom] = useState(100);

  return (
    <WidgetRoot className="pdf-viewer" styleProps={styleProps}>
      <div className="pdf-toolbar" aria-label="PDF viewer controls">
        <button
          disabled={page <= 1}
          onClick={() => setPage((current) => Math.max(1, current - 1))}
          type="button"
        >
          Previous
        </button>
        <span>
          Page {page} of {pageCount}
        </span>
        <button
          disabled={page >= pageCount}
          onClick={() => setPage((current) => Math.min(pageCount, current + 1))}
          type="button"
        >
          Next
        </button>
        <button
          onClick={() => setZoom((current) => Math.max(minZoom, current - zoomStep))}
          type="button"
        >
          Zoom out
        </button>
        <span>{zoom}%</span>
        <button
          onClick={() => setZoom((current) => Math.min(maxZoom, current + zoomStep))}
          type="button"
        >
          Zoom in
        </button>
      </div>
      {config.src !== undefined && config.src.length > 0 ? (
        <iframe className="pdf-frame" src={config.src} title={config.title} />
      ) : (
        <div className="pdf-page" style={{ transform: `scale(${zoom / 100})` }}>
          <strong>{config.label}</strong>
          <span>Page {page}</span>
          <p>
            Simulated document preview for policies, signed packets, offer letters,
            pay-band guidance, and uploaded evidence.
          </p>
        </div>
      )}
    </WidgetRoot>
  );
}

export function MediaWidget({
  config,
  styleProps,
}: WidgetComponentProps<MediaConfig>): JSX.Element {
  if (config.mediaType === "image") {
    return <ImageMediaWidget config={config} styleProps={styleProps} />;
  }

  if (config.mediaType === "audio") {
    return <AudioMediaWidget config={config} styleProps={styleProps} />;
  }

  if (config.mediaType === "video") {
    return <VideoMediaWidget config={config} styleProps={styleProps} />;
  }

  return <PdfViewerWidget config={config} styleProps={styleProps} />;
}

export const MediaViewerWidget = MediaWidget;

export function DocumentPreviewWidget({
  config,
  styleProps,
}: WidgetComponentProps<DocumentPreviewConfig>): JSX.Element {
  return <PdfViewerWidget config={config} styleProps={styleProps} />;
}
