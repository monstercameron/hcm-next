import { Sparkles, X } from "lucide-react";
import {
  useCallback,
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
  type KeyboardEvent,
  type ReactNode,
} from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import type { AppError } from "@hcm-next/foundation";
import type { PageDefinition } from "@hcm-next/ui-contracts";
import {
  defaultQuickActions,
  renderPromptTemplate,
  resolveAvailableChips,
  type QuickAction,
} from "./chat-panel-helpers.js";
import { useChat, type ChatMessage } from "./use-chat.js";

export type { QuickAction } from "./chat-panel-helpers.js";
export type { ChatMessage } from "./use-chat.js";

/**
 * Props for {@link AiChatPanel}. The panel is fully controlled by its own
 * internal state — callers only need to wire `onGenerated` to receive the
 * resulting page, plus the actor/intent context required to compose the
 * request payload.
 */
export type AiChatPanelProps = {
  /** Fires whenever the assistant resolves a page. Source is always `"fresh"`
   * because chat responses are dynamic and never cached. */
  onGenerated: (page: PageDefinition, source: "cache" | "fresh") => void;
  /** Actor record identifier sent to the API as `x-demo-actor-id` so the
   * backend resolves the correct permission set. Omit in tests that don't
   * exercise the network path. */
  actorId?: string;
  /** Intents the actor can drive; chips for unknown intents are hidden. */
  availableIntents: readonly string[];
  /** Default subject identifier used when no chip overrides it. */
  defaultSubjectId?: string;
  /** Default subject display name used in templated prompts. */
  defaultSubjectName?: string;
  /** Override the default action chips; falls back to the canonical set. */
  quickActions?: readonly QuickAction[];
};

const IDLE_DELAY_MS = 30_000;
const TEXTAREA_MAX_LINES = 5;
const TEXTAREA_BASE_LINE_HEIGHT_PX = 22;
const EMPTY_MESSAGE = "Tell me what you'd like to do, or pick an action above.";

const FOCUSABLE_SELECTOR = [
  "a[href]",
  "button:not([disabled])",
  "textarea:not([disabled])",
  "input:not([disabled])",
  "select:not([disabled])",
  "[tabindex]:not([tabindex='-1'])",
].join(",");

/**
 * Floating HR-assistant entry point. Owns its own open/close, prompt, and
 * conversation state; defers each chat turn to {@link useChat} and bubbles
 * any rendered `PageDefinition` to the caller via `onGenerated`.
 *
 * The voice is deliberately "HR coordinator" rather than "AI tool" — labels
 * avoid AI/ML jargon. The panel is a conversational surface — there is no
 * client-side validation of the user's prompt; the agent itself decides when
 * to ask clarifying questions vs. take action.
 */
export function AiChatPanel({
  onGenerated,
  actorId,
  availableIntents,
  defaultSubjectName,
  quickActions,
}: AiChatPanelProps): JSX.Element {
  const [isOpen, setIsOpen] = useState(false);
  const [userPrompt, setUserPrompt] = useState("");
  const [messages, setMessages] = useState<readonly ChatMessage[]>([]);
  const [isIdle, setIsIdle] = useState(false);

  const titleId = useId();
  const fabRef = useRef<HTMLButtonElement | null>(null);
  const panelRef = useRef<HTMLDivElement | null>(null);
  const closeButtonRef = useRef<HTMLButtonElement | null>(null);
  const textareaRef = useRef<HTMLTextAreaElement | null>(null);
  const conversationRef = useRef<HTMLDivElement | null>(null);
  const idleTimerRef = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);

  const chat = useChat();

  const visibleChips = useMemo(
    () => resolveAvailableChips(quickActions, availableIntents),
    [quickActions, availableIntents],
  );

  const resetIdleTimer = useCallback(() => {
    setIsIdle(false);
    if (idleTimerRef.current !== undefined) {
      clearTimeout(idleTimerRef.current);
    }
    idleTimerRef.current = setTimeout(() => {
      setIsIdle(true);
    }, IDLE_DELAY_MS);
  }, []);

  useEffect(() => {
    resetIdleTimer();
    return () => {
      if (idleTimerRef.current !== undefined) {
        clearTimeout(idleTimerRef.current);
      }
    };
  }, [resetIdleTimer]);

  // Autosize the textarea up to TEXTAREA_MAX_LINES.
  useEffect(() => {
    const node = textareaRef.current;
    if (node === null) {
      return;
    }
    node.style.height = "auto";
    const maxHeight = TEXTAREA_BASE_LINE_HEIGHT_PX * TEXTAREA_MAX_LINES;
    node.style.height = `${Math.min(node.scrollHeight, maxHeight)}px`;
  }, [userPrompt, isOpen]);

  // Auto-scroll the conversation to the bottom when new turns land.
  useEffect(() => {
    const node = conversationRef.current;
    if (node === null) {
      return;
    }
    node.scrollTo({ top: node.scrollHeight, behavior: "smooth" });
  }, [messages, chat.isPending]);

  // Move focus to the close button when the panel opens.
  useEffect(() => {
    if (!isOpen) {
      return;
    }
    closeButtonRef.current?.focus();
  }, [isOpen]);

  const closePanel = useCallback(() => {
    setIsOpen(false);
    // Restore focus to the floating button so keyboard users keep their place.
    fabRef.current?.focus();
  }, []);

  // Escape closes; Tab cycles focus within the panel (focus trap).
  useEffect(() => {
    if (!isOpen) {
      return;
    }
    const handleKeyDown = (event: globalThis.KeyboardEvent): void => {
      resetIdleTimer();
      if (event.key === "Escape") {
        event.preventDefault();
        closePanel();
        return;
      }
      if (event.key !== "Tab") {
        return;
      }
      const root = panelRef.current;
      if (root === null) {
        return;
      }
      const focusables = Array.from(
        root.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR),
      ).filter((element) => !element.hasAttribute("data-focus-skip"));
      if (focusables.length === 0) {
        return;
      }
      const first = focusables[0];
      const last = focusables[focusables.length - 1];
      if (first === undefined || last === undefined) {
        return;
      }
      const active = document.activeElement as HTMLElement | null;
      if (event.shiftKey) {
        if (active === first || active === root) {
          event.preventDefault();
          last.focus();
        }
        return;
      }
      if (active === last) {
        event.preventDefault();
        first.focus();
      }
    };
    document.addEventListener("keydown", handleKeyDown);
    return () => document.removeEventListener("keydown", handleKeyDown);
  }, [isOpen, closePanel, resetIdleTimer]);

  // Click outside the panel closes it.
  useEffect(() => {
    if (!isOpen) {
      return;
    }
    const handleMouseDown = (event: MouseEvent): void => {
      const root = panelRef.current;
      const fab = fabRef.current;
      const target = event.target as Node | null;
      if (target === null) {
        return;
      }
      if (root !== null && root.contains(target)) {
        return;
      }
      if (fab !== null && fab.contains(target)) {
        return;
      }
      closePanel();
    };
    document.addEventListener("mousedown", handleMouseDown);
    return () => document.removeEventListener("mousedown", handleMouseDown);
  }, [isOpen, closePanel]);

  const handleToggleOpen = (): void => {
    resetIdleTimer();
    setIsOpen((previous) => !previous);
  };

  const handleChipActivate = (chip: QuickAction): void => {
    resetIdleTimer();
    const subjectNameForTemplate = chip.defaultSubjectName ?? defaultSubjectName;
    const renderedPrompt = renderPromptTemplate(
      chip.promptTemplate,
      subjectNameForTemplate === undefined
        ? {}
        : { subjectName: subjectNameForTemplate },
    );
    setUserPrompt(renderedPrompt);
    // Defer focus to the next paint so the textarea has a chance to mount.
    queueMicrotask(() => {
      textareaRef.current?.focus();
      const node = textareaRef.current;
      if (node !== null) {
        node.selectionStart = node.value.length;
        node.selectionEnd = node.value.length;
      }
    });
  };

  const handleChipKeyDown = (
    chip: QuickAction,
    event: KeyboardEvent<HTMLButtonElement>,
  ): void => {
    if (event.key === "Enter" || event.key === " ") {
      event.preventDefault();
      handleChipActivate(chip);
    }
  };

  const submitPrompt = (): void => {
    resetIdleTimer();
    const trimmedPrompt = userPrompt.trim();
    if (trimmedPrompt.length === 0) {
      return;
    }
    if (chat.isPending) {
      return;
    }

    const nextMessage: ChatMessage = {
      role: "user",
      content: trimmedPrompt,
    };
    const nextMessages: ChatMessage[] = [...messages, nextMessage];
    setMessages(nextMessages);
    setUserPrompt("");

    chat.mutate(
      {
        messages: nextMessages,
        ...(actorId !== undefined ? { actorId } : {}),
      },
      {
        onSuccess: (output) => {
          setMessages((previous) => [...previous, output.assistantMessage]);
          const renderPage = output.sideEffects.renderPage;
          if (renderPage !== undefined) {
            onGenerated(renderPage, "fresh");
          }
        },
        onError: (error: AppError) => {
          const errorMessage: ChatMessage = {
            role: "assistant",
            content: error.safeMessage,
          };
          setMessages((previous) => [...previous, errorMessage]);
        },
      },
    );
  };

  const handleTextareaKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>): void => {
    resetIdleTimer();
    // Enter submits; Shift+Enter inserts a newline. Cmd/Ctrl+Enter still works
    // for muscle-memory carry-over from the previous bindings.
    if (event.key !== "Enter") {
      return;
    }
    if (event.shiftKey) {
      return;
    }
    event.preventDefault();
    submitPrompt();
  };

  const submitDisabled = chat.isPending || userPrompt.trim().length === 0;

  return (
    <>
      <button
        aria-expanded={isOpen}
        aria-label="Open HCM assistant"
        className="ai-chat-fab"
        data-idle={isIdle ? "true" : "false"}
        onClick={handleToggleOpen}
        ref={fabRef}
        type="button"
      >
        {isOpen ? <X aria-hidden size={20} /> : <Sparkles aria-hidden size={20} />}
      </button>

      {isOpen ? (
        <div
          aria-labelledby={titleId}
          aria-modal="true"
          className="ai-chat-panel ai-chat-panel-open"
          ref={panelRef}
          role="dialog"
        >
          <header className="ai-chat-panel-header">
            <div className="ai-chat-panel-brand">
              <span aria-hidden className="ai-chat-panel-glyph">
                <Sparkles size={18} />
              </span>
              <div className="ai-chat-panel-titles">
                <p className="ai-chat-panel-title" id={titleId}>
                  HCM Assistant
                </p>
                <p className="ai-chat-panel-subtitle">How can I help?</p>
              </div>
            </div>
            <button
              aria-label="Close assistant"
              className="ai-chat-panel-close"
              onClick={closePanel}
              ref={closeButtonRef}
              type="button"
            >
              <X aria-hidden size={18} />
            </button>
          </header>

          {visibleChips.length > 0 ? (
            <div
              aria-label="Quick actions"
              className="ai-chat-quick-actions"
              role="group"
            >
              {visibleChips.map((chip) => (
                <button
                  className="ai-chat-chip"
                  key={chip.id}
                  onClick={() => handleChipActivate(chip)}
                  onKeyDown={(event) => handleChipKeyDown(chip, event)}
                  type="button"
                >
                  {chip.label}
                </button>
              ))}
            </div>
          ) : null}

          <div
            aria-live="polite"
            className="ai-chat-conversation"
            ref={conversationRef}
          >
            {messages.length === 0 && !chat.isPending ? (
              <p className="ai-chat-empty">{EMPTY_MESSAGE}</p>
            ) : (
              <>
                {messages.map((message, index) => (
                  <ConversationMessage
                    key={`${message.role}-${index}`}
                    message={message}
                  />
                ))}
                {chat.isPending ? (
                  <div className="ai-chat-turn">
                    <div className="ai-chat-turn-assistant">
                      <span className="ai-chat-status ai-chat-status-generating">
                        Generating...
                      </span>
                    </div>
                  </div>
                ) : null}
              </>
            )}
          </div>

          <div className="ai-chat-input-row">
            <textarea
              aria-label="Describe what you'd like to do"
              className="ai-chat-input"
              onChange={(event) => {
                resetIdleTimer();
                setUserPrompt(event.currentTarget.value);
              }}
              onKeyDown={handleTextareaKeyDown}
              placeholder="Tell me what you'd like to do..."
              ref={textareaRef}
              rows={1}
              value={userPrompt}
            />
            <button
              className="ai-chat-submit"
              disabled={submitDisabled}
              onClick={submitPrompt}
              type="button"
            >
              Go
            </button>
          </div>
        </div>
      ) : null}
    </>
  );
}

function ConversationMessage({ message }: { message: ChatMessage }): ReactNode {
  if (message.role === "user") {
    return (
      <div className="ai-chat-turn">
        <div className="ai-chat-turn-user">{message.content}</div>
      </div>
    );
  }
  return (
    <div className="ai-chat-turn">
      <div className="ai-chat-turn-assistant ai-chat-markdown">
        <ReactMarkdown
          remarkPlugins={[remarkGfm]}
          components={{
            // Links open in a new tab so the chat doesn't navigate away.
            a: ({ children, ...props }) => (
              <a {...props} rel="noopener noreferrer" target="_blank">
                {children}
              </a>
            ),
          }}
        >
          {message.content}
        </ReactMarkdown>
      </div>
    </div>
  );
}

// Re-export default quick actions so tests / WS7 can introspect the catalog.
export { defaultQuickActions };
