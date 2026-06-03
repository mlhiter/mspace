Packet ID: P2-implementation

Accepted:
- Added `TestsModal` for local test-module dialogs with backdrop close, close button, Escape handling, labels, and scroll-safe sizing.
- Changed the Cases toolbar import action to open an import dialog using the existing import mutation.
- Changed the Cases toolbar new-case action to open a create dialog using the existing create mutation.
- Added `CaseFormFields` so create dialog and detail page share the same case fields.
- Added English and Simplified Chinese strings for dialog title, description, and close labels.

Rejected:
- Removing `/tests/cases/new`; it remains a valid deep-link route.
- Changing backend import/create contract.

Remaining risks:
- Visual browser smoke not yet performed in this packet.
