/* New session — placement dialog. Pick a type, name it, then place on a fit-ranked machine. */
const SESSION_TYPES = [
  { id: "terminal", icon: "terminal", name: "Terminal", desc: "A shell on the node" },
  { id: "claude", icon: "sparkles", name: "Claude", desc: "AI-paired session" },
  { id: "editor", icon: "file-code", name: "Editor", desc: "VS Code + terminal" },
];

function NewSession({ fleet, onClose, onStart }) {
  const ranked = React.useMemo(() => rankByFit(fleet), [fleet]);
  const firstPlaceable = ranked.find(m => !m.offline) || ranked[0];
  const [type, setType] = React.useState("claude");
  const [name, setName] = React.useState("");
  const [pick, setPick] = React.useState(firstPlaceable.id);
  const typeMeta = SESSION_TYPES.find(t => t.id === type);
  const placeholder = type === "terminal" ? "build-watcher" : type === "editor" ? "edit-mesh" : "pair-on-mesh";

  return (
    <div className="scrim" onClick={onClose}>
      <div className="modal wide" onClick={e => e.stopPropagation()}>
        <h3>New session</h3>
        <div className="sub">Lattice places it on the best machine in your mesh. It survives sleep and disconnects — reattach from any node.</div>

        <label className="flabel">Session type</label>
        <div className="type-grid">
          {SESSION_TYPES.map(t => (
            <button key={t.id} className={`type-opt ${type === t.id ? "on" : ""}`} onClick={() => setType(t.id)}>
              <span className="ti"><Icon name={t.icon} size={16} /></span>
              <span className="tn">{t.name}</span>
              <span className="td">{t.desc}</span>
            </button>
          ))}
        </div>

        <label className="flabel">Name</label>
        <input className="field mono" placeholder={placeholder} value={name} onChange={e => setName(e.target.value)} autoFocus />

        <label className="flabel">
          Place on
          <span className="hint">ranked by free RAM · load · locality</span>
        </label>
        <div className="rank-list">
          {ranked.map((m, i) => {
            const score = fitScore(m);
            const freeRAM = m.memTotal - m.memUsed;
            const locName = m.locality === 0 ? "this mac" : m.locality === 1 ? "LAN" : "remote";
            const best = !m.offline && i === 0;
            return (
              <div
                key={m.id}
                className={`rank ${pick === m.id ? "on" : ""} ${m.offline ? "dis" : ""}`}
                onClick={() => !m.offline && setPick(m.id)}
              >
                <span className="badge">{m.offline ? "—" : i + 1}</span>
                <div>
                  <div className="id-line">
                    <Dot status={m.status} />
                    <Icon name={m.kind} size={14} color="var(--fg-3)" />
                    <span className="nm">{m.label}</span>
                    {best && <Chip kind="alive">Best fit</Chip>}
                    {pick === m.id && !best && <Chip kind="cool">Selected</Chip>}
                  </div>
                  {m.offline ? (
                    <div className="stats"><div className="stat"><span className="v" style={{ color: "var(--fg-3)" }}>Offline · wake in Fleet to place here</span></div></div>
                  ) : (
                    <div className="stats">
                      <div className="stat"><span className="k">Free RAM</span><span className="v good">{freeRAM.toFixed(0)} GB</span></div>
                      <div className="stat"><span className="k">Load</span><span className="v">{m.cpu}% · {m.cores}c</span></div>
                      <div className="stat"><span className="k">Locality</span><span className="v">{locName}</span></div>
                    </div>
                  )}
                </div>
                <div className="fit">
                  <span className="score">{m.offline ? "—" : score}</span>
                  <div className="fitbar"><i style={{ width: (m.offline ? 0 : score) + "%" }} /></div>
                </div>
              </div>
            );
          })}
        </div>

        <div style={{ display: "flex", alignItems: "center", marginTop: 22 }}>
          <span className="mono" style={{ fontSize: 11, color: "var(--fg-3)" }}>
            {typeMeta.name} → <span style={{ color: "var(--green)" }}>{fleet.find(m => m.id === pick)?.label}</span>
          </span>
          <div style={{ flex: 1 }} />
          <button className="btn btn-ghost" onClick={onClose}>Cancel</button>
          <button className="btn btn-run" style={{ marginLeft: 8 }} onClick={() => onStart(pick, name || placeholder, type)}>
            <Icon name="play" />Start session
          </button>
        </div>
      </div>
    </div>
  );
}
window.NewSession = NewSession;
