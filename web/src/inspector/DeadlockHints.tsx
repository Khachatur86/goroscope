import type { DeadlockHint } from "../api/client";

type Props = {
  hints: DeadlockHint[];
  onSelectGoroutine: (id: number) => void;
};

/** Renders a blame chain string with clickable G{n} goroutine IDs */
function BlameChainText({
  text,
  onSelectGoroutine,
}: {
  text: string;
  onSelectGoroutine: (id: number) => void;
}) {
  const parts: React.ReactNode[] = [];
  const re = /G(\d+)/g;
  let lastIndex = 0;
  let match: RegExpExecArray | null;
  let keyIdx = 0;
  while ((match = re.exec(text)) !== null) {
    parts.push(text.slice(lastIndex, match.index));
    const gid = parseInt(match[1], 10);
    parts.push(
      <button
        key={`g-${keyIdx}`}
        type="button"
        className="deadlock-blame-gid"
        onClick={() => onSelectGoroutine(gid)}
      >
        G{gid}
      </button>
    );
    keyIdx += 1;
    lastIndex = match.index + match[0].length;
  }
  parts.push(text.slice(lastIndex));
  return <>{parts}</>;
}

export function DeadlockHints({ hints, onSelectGoroutine }: Props) {
  if (hints.length === 0) {
    return (
      <div className="inspector-section">
        <div className="inspector-label">Deadlock Hints</div>
        <p className="empty-message">No potential deadlock cycles detected.</p>
      </div>
    );
  }

  return (
    <div className="inspector-section">
      <div className="inspector-label">Deadlock Hints</div>
      <p className="inspector-hint">
        Potential cycles where all goroutines are blocked. Click a goroutine ID to select it.
      </p>
      <div className="deadlock-hints-list">
        {hints.map((hint, i) => (
          <div key={i} className="deadlock-hint-block">
            <div className="deadlock-hint-chain">
              {hint.blame_chain ? (
                <BlameChainText
                  text={hint.blame_chain}
                  onSelectGoroutine={onSelectGoroutine}
                />
              ) : (
                <span>
                  Cycle:{" "}
                  {hint.goroutine_ids.map((id) => (
                    <button
                      key={id}
                      type="button"
                      className="deadlock-blame-gid"
                      onClick={() => onSelectGoroutine(id)}
                    >
                      G{id}
                    </button>
                  ))}
                  {hint.resource_ids?.length ? ` · Resources: ${hint.resource_ids.join(", ")}` : ""}
                </span>
              )}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
