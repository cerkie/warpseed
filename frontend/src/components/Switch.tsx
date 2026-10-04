/** An on/off switch. With children it shows them as its label; without, it is
    just the switch and `label` names it for screen readers (use that inside a
    settings row, where the row already shows the name). */
export default function Switch({
  checked,
  onChange,
  children,
  label,
}: {
  checked: boolean;
  onChange: (on: boolean) => void;
  children?: React.ReactNode;
  label?: string;
}) {
  return (
    <label className="switch">
      <input
        type="checkbox"
        role="switch"
        checked={checked}
        aria-label={label}
        onChange={(e) => onChange(e.target.checked)}
      />
      <span className="switch__track" aria-hidden="true" />
      {children && <span className="switch__label">{children}</span>}
    </label>
  );
}
