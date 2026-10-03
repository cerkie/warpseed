import { friendlyError } from "../lib/errors";
import { importSitesFromFile } from "../lib/sites";

/** The strip that tells the user they can bring their sites over from
    FileZilla or WinSCP: drop the export file on the window, or pick it. The
    drop itself is handled in lib/fileDrop.ts. */
export default function ImportHint({ onMessage }: { onMessage: (text: string, isError: boolean) => void }) {
  return (
    <div className="import-hint">
      <span>
        Coming from FileZilla or WinSCP? Drop your exported sites file (.xml or .ini) here, or{" "}
        <button
          type="button"
          className="import-hint__link"
          onClick={() =>
            void importSitesFromFile()
              .then((m) => m && onMessage(m, false))
              .catch((err: unknown) => onMessage(friendlyError(err), true))
          }
        >
          choose a file
        </button>
        .
      </span>
    </div>
  );
}
