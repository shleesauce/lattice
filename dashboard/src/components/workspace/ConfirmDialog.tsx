// In-app confirm modal, styled with the Living Mesh tokens (.scrim/.modal) —
// replaces the default browser window.confirm for destructive actions.
interface Props {
  title: string
  body: string
  confirmLabel: string
  danger?: boolean
  onConfirm: () => void
  onClose: () => void
}

export function ConfirmDialog({ title, body, confirmLabel, danger, onConfirm, onClose }: Props) {
  return (
    <div className="scrim" onClick={onClose}>
      <div className="modal" style={{ width: 420 }} onClick={(e) => e.stopPropagation()}>
        <h3>{title}</h3>
        <div className="sub" style={{ marginBottom: 20 }}>
          {body}
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <div style={{ flex: 1 }} />
          <button className="btn btn-secondary" type="button" onClick={onClose}>
            Cancel
          </button>
          <button
            className={danger ? 'btn btn-danger' : 'btn btn-primary'}
            type="button"
            onClick={() => {
              onConfirm()
              onClose()
            }}
          >
            {confirmLabel}
          </button>
        </div>
      </div>
    </div>
  )
}
