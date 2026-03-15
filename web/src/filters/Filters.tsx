type FiltersState = {
  state: string;
  reason: string;
  resource: string;
  search: string;
  minWaitNs: string;
  sortMode: string;
};

type Props = {
  filters: FiltersState;
  onFiltersChange: (f: FiltersState) => void;
  onJumpTo: (id: number) => void;
  jumpToInputRef?: React.RefObject<HTMLInputElement>;
};

export function Filters({ filters, onFiltersChange, onJumpTo, jumpToInputRef }: Props) {
  const handleJump = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Enter") {
      const id = parseInt((e.target as HTMLInputElement).value, 10);
      if (Number.isFinite(id) && id > 0) {
        onJumpTo(id);
        (e.target as HTMLInputElement).value = "";
      }
    }
  };

  const applyPreset = (preset: "all" | "blocked" | "channels" | "mutex") => {
    if (preset === "all") {
      onFiltersChange({ ...filters, state: "ALL", reason: "" });
    } else if (preset === "blocked") {
      onFiltersChange({ ...filters, state: "BLOCKED", reason: "" });
    } else if (preset === "channels") {
      onFiltersChange({ ...filters, state: "ALL", reason: "chan_send" });
    } else if (preset === "mutex") {
      onFiltersChange({ ...filters, state: "ALL", reason: "mutex_lock" });
    }
  };

  const clearFilters = () => {
    onFiltersChange({
      state: "ALL",
      reason: "",
      resource: "",
      search: "",
      minWaitNs: "",
      sortMode: filters.sortMode,
    });
  };

  return (
    <>
      <div className="filter-presets">
        <button type="button" className="preset-chip" onClick={() => applyPreset("all")}>
          All
        </button>
        <button type="button" className="preset-chip" onClick={() => applyPreset("blocked")}>
          Blocked
        </button>
        <button type="button" className="preset-chip" onClick={() => applyPreset("channels")}>
          Channels
        </button>
        <button type="button" className="preset-chip" onClick={() => applyPreset("mutex")}>
          Mutex
        </button>
      </div>
      <div className="filter-stack">
        <label className="field">
          <span>Jump to G</span>
          <input
            ref={jumpToInputRef}
            type="number"
            placeholder="goroutine ID"
            min={1}
            onKeyDown={handleJump}
          />
        </label>
        <label className="field">
          <span>Search</span>
          <input
            type="search"
            placeholder="id, function, reason"
            value={filters.search}
            onChange={(e) =>
              onFiltersChange({ ...filters, search: e.target.value })
            }
          />
        </label>
        <label className="field">
          <span>State</span>
          <select
            value={filters.state}
            onChange={(e) =>
              onFiltersChange({ ...filters, state: e.target.value })
            }
          >
            <option value="ALL">All states</option>
            <option value="RUNNING">RUNNING</option>
            <option value="RUNNABLE">RUNNABLE</option>
            <option value="WAITING">WAITING</option>
            <option value="BLOCKED">BLOCKED</option>
            <option value="SYSCALL">SYSCALL</option>
            <option value="DONE">DONE</option>
          </select>
        </label>
        <label className="field">
          <span>Reason</span>
          <select
            value={filters.reason}
            onChange={(e) =>
              onFiltersChange({ ...filters, reason: e.target.value })
            }
          >
            <option value="">Any</option>
            <option value="chan_send">chan_send</option>
            <option value="chan_recv">chan_recv</option>
            <option value="select">select</option>
            <option value="mutex_lock">mutex_lock</option>
            <option value="sync_cond">sync_cond</option>
            <option value="syscall">syscall</option>
            <option value="sleep">sleep</option>
            <option value="gc_assist">gc_assist</option>
            <option value="unknown">unknown</option>
          </select>
        </label>
        <label className="field">
          <span>Resource</span>
          <input
            type="text"
            placeholder="channel/mutex id"
            value={filters.resource}
            onChange={(e) =>
              onFiltersChange({ ...filters, resource: e.target.value })
            }
          />
        </label>
        <label className="field">
          <span>Min wait</span>
          <select
            value={filters.minWaitNs}
            onChange={(e) =>
              onFiltersChange({ ...filters, minWaitNs: e.target.value })
            }
          >
            <option value="">Any</option>
            <option value="100000000">≥100ms</option>
            <option value="500000000">≥500ms</option>
            <option value="1000000000">≥1s</option>
            <option value="5000000000">≥5s</option>
          </select>
        </label>
        <label className="field">
          <span>Sort</span>
          <select
            value={filters.sortMode}
            onChange={(e) =>
              onFiltersChange({ ...filters, sortMode: e.target.value })
            }
          >
            <option value="SUSPICIOUS">Most suspicious</option>
            <option value="BLOCKED">Most blocked</option>
            <option value="WAIT_TIME">Longest wait</option>
            <option value="ID">Goroutine ID</option>
          </select>
        </label>
        <button type="button" className="filter-clear" onClick={clearFilters}>
          Clear
        </button>
      </div>
    </>
  );
}
