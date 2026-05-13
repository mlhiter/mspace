import { useEffect, useMemo } from "react";
import type { ReactNode } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate, useParams } from "@tanstack/react-router";
import { CheckCircle2, MailPlus, ShieldCheck } from "lucide-react";
import { controlPlaneApi, queryKeys, SELECTED_WORKSPACE_STORAGE_KEY } from "@mspace/core";
import { Button, Notice, PageFrame } from "@mspace/ui";
import { useMspaceAuth } from "./auth-context";
import { RelativeTime } from "./time";

export function WorkspaceInvitePage() {
	const { token = "" } = useParams({ strict: false }) as { token?: string };
	const auth = useMspaceAuth();
	const queryClient = useQueryClient();
	const navigate = useNavigate();
	const acceptInvite = useMutation({
		mutationFn: () => controlPlaneApi.acceptWorkspaceInvitation(auth.token, token),
		onSuccess: async (result) => {
			window.localStorage.setItem(SELECTED_WORKSPACE_STORAGE_KEY, result.workspace.id);
			auth.selectWorkspace?.(result.workspace.id);
			await queryClient.invalidateQueries({ queryKey: queryKeys.authMe(auth.token) });
		},
	});
	const accepted = acceptInvite.data;
	const canAccept = auth.status === "signed-in" && auth.token !== "" && token !== "" && !accepted;
	const inviteTokenPrefix = useMemo(() => token.slice(0, 12), [token]);

	useEffect(() => {
		if (accepted) {
			void queryClient.invalidateQueries({ queryKey: queryKeys.workspaceMembers(accepted.workspace.id, auth.token) });
		}
	}, [accepted, auth.token, queryClient]);

	return (
		<PageFrame
			title="Join workspace"
			subtitle="Accept an invite link to add this GitHub identity to a shared mspace workspace."
			actions={
				accepted ? (
					<Button type="button" onClick={() => navigate({ to: "/settings" })}>
						<ShieldCheck data-icon />
						Open workspace
					</Button>
				) : null
			}
		>
			<div className="mx-auto grid max-w-[720px] gap-4">
				<div className="rounded-[10px] bg-[color:var(--surface)] px-5 py-5 shadow-[inset_0_0_0_1px_var(--line)]">
					<div className="flex min-w-0 items-start gap-3">
						<div className="grid size-10 shrink-0 place-items-center rounded-[10px] bg-[color:var(--block)] text-[color:var(--muted)] shadow-[inset_0_0_0_1px_var(--line)]">
							<MailPlus data-icon />
						</div>
						<div className="min-w-0 flex-1">
							<h2 className="text-[16px] font-semibold leading-6 text-[color:var(--text)]">
								{accepted ? `Joined ${accepted.workspace.name}` : "Workspace invite"}
							</h2>
							<p className="mt-1 text-[13px] leading-6 text-[color:var(--muted)] text-pretty">
								{accepted
									? "This account can now use the shared workspace, team runtime registry, and server-backed Inbox receipts."
									: "Sign in with GitHub, then accept the invite to join the workspace on this machine."}
							</p>
						</div>
					</div>

					<div className="mt-5 grid gap-3 rounded-[9px] bg-[color:var(--block)] p-3 shadow-[inset_0_0_0_1px_var(--line)]">
						<InviteFact label="Invite token" value={inviteTokenPrefix || "missing"} mono />
						<InviteFact label="Current account" value={auth.user?.name || auth.user?.email || "Not signed in"} />
						{accepted ? (
							<>
								<InviteFact label="Workspace" value={accepted.workspace.name} />
								<InviteFact label="Role" value={accepted.workspace.role} />
								<InviteFact label="Accepted" value={<RelativeTime value={accepted.invitation.acceptedAt} />} />
							</>
						) : null}
					</div>

					{auth.status !== "signed-in" ? (
						<div className="mt-4">
							<Notice>Use the workspace menu in the left rail to sign in with GitHub, then return to this invite page.</Notice>
						</div>
					) : null}
					{acceptInvite.error ? (
						<div className="mt-4">
							<Notice tone="danger">{acceptInvite.error.message}</Notice>
						</div>
					) : null}
					{accepted ? (
						<div className="mt-4 flex items-center gap-2 rounded-[8px] bg-[color:var(--success-soft)] px-3 py-2 text-[13px] font-medium leading-5 text-[color:var(--success)]">
							<CheckCircle2 data-icon />
							Invite accepted
						</div>
					) : (
						<div className="mt-5 flex justify-end">
							<Button type="button" disabled={!canAccept || acceptInvite.isPending} onClick={() => acceptInvite.mutate()}>
								<MailPlus data-icon />
								{acceptInvite.isPending ? "Joining" : "Join workspace"}
							</Button>
						</div>
					)}
				</div>
			</div>
		</PageFrame>
	);
}

function InviteFact(props: { label: string; value: ReactNode; mono?: boolean }) {
	return (
		<div className="grid gap-1 sm:grid-cols-[120px_minmax(0,1fr)] sm:items-center">
			<div className="text-[12px] font-medium leading-5 text-[color:var(--faint)]">{props.label}</div>
			<div className={props.mono ? "truncate font-mono text-[12px] text-[color:var(--muted-strong)]" : "truncate text-[13px] text-[color:var(--text)]"}>
				{props.value}
			</div>
		</div>
	);
}
