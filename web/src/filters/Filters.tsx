type FiltersState = {
  state: string;
  reason: string;
  search: string;
  minWaitNs: string;
};

type Props = {
  filters: FiltersState;
  onFiltersChange: (f: FiltersState) => void;
  onJumpTo: (id: number) => void;
};

export function Filters({ filters, onFiltersChange, onJumpTo }: Props) {
  const handleJump = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Enter") {
      const id = parseInt((e.target as HTMLInputElement).value, 10);
      if (Number.isFinite(id) && id > 0) {
        onJumpTo(id);
        (e.target as HTMLInputElement).value = "";
      }
    }
  };

  return (
    <div className="filter-stack">
      <label className="field">
        <span>Jump to G</span>
        <input
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
          <option value="mutex_lock">mutex_lock</option>
          <option value="sync_cond">sync_cond</option>
          <option value="unknown">unknown</option>
        </select>
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
          <option value="1000000000">≥1s</option>
        </select>
      </label>
    </div>
  );
}
