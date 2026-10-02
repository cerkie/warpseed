export default function AutoConnectField({
  checked,
  onChange,
}: {
  checked: boolean;
  onChange: (on: boolean) => void;
}) {
  return (
    <label className="set-check wide">
      <input type="checkbox" checked={checked} onChange={(e) => onChange(e.target.checked)} />
      Connect to this site when warpseed starts
    </label>
  );
}
