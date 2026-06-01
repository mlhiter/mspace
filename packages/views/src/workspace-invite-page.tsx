import { useEffect, useMemo } from "react";
import type { ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate, useParams } from "@tanstack/react-router";
import { CheckCircle2, MailPlus, ShieldCheck } from "lucide-react";
import { controlPlaneApi, queryKeys, SELECTED_WORKSPACE_STORAGE_KEY, type AuthMeResult } from "@mspace/core";
import { Button, Notice, PageFrame, cn } from "@mspace/ui";
import { useMspaceTranslation } from "@mspace/i18n";
import { useMspaceAuth } from "./auth-context";
import { RelativeTime } from "./time";

export function WorkspaceInvitePage() {
	const { t } = useMspaceTranslation();
	const { token = "" } = useParams({ strict: false }) as { token?: string };
	const auth = useMspaceAuth();
	const queryClient = useQueryClient();
	const navigate = useNavigate();
	const invitePreview = useQuery({
		queryKey: ["workspace-invitation-preview", token],
		queryFn: () => controlPlaneApi.previewWorkspaceInvitation(token),
		enabled: token !== "",
		retry: false,
		refetchOnWindowFocus: false,
	});
	const acceptInvite = useMutation({
		mutationFn: () => controlPlaneApi.acceptWorkspaceInvitation(auth.token, token),
		onSuccess: async (result) => {
			const authMeKey = queryKeys.authMe(auth.token);
			queryClient.setQueriesData<AuthMeResult | undefined>({ queryKey: authMeKey }, (current) => {
				if (!current) return current;
				const hasAcceptedWorkspace = result.workspaces.some((workspace) => workspace.id === result.workspace.id);
				return {
					...current,
					workspaces: hasAcceptedWorkspace ? result.workspaces : [result.workspace, ...result.workspaces],
				};
			});
			window.localStorage.setItem(SELECTED_WORKSPACE_STORAGE_KEY, result.workspace.id);
			auth.selectWorkspace?.(result.workspace.id);
			await queryClient.invalidateQueries({ queryKey: authMeKey });
			window.localStorage.setItem(SELECTED_WORKSPACE_STORAGE_KEY, result.workspace.id);
			auth.selectWorkspace?.(result.workspace.id);
			void navigate({ to: "/inbox" });
		},
	});
	const accepted = acceptInvite.data;
	const canAccept = auth.status === "signed-in" && auth.token !== "" && token !== "" && !accepted && invitePreview.data?.status === "pending";
	const currentAccountLabel = useMemo(() => auth.user?.name || auth.user?.email || t("workspaceInvite.notSignedIn"), [auth.user?.email, auth.user?.name, t]);
	const inviterName = invitePreview.data?.invitedByName || invitePreview.data?.invitedByLogin || t("workspaceInvite.unknownInviter");
	const inviteBody = auth.status === "signed-in"
		? t("workspaceInvite.inviteBodySignedIn", { account: currentAccountLabel })
		: t("workspaceInvite.inviteBodySignedOut");

	useEffect(() => {
		if (token) {
			void window.mspaceDesktop?.setPendingInviteToken?.(token);
		}
	}, [token]);

	useEffect(() => {
		if (accepted) {
			void window.mspaceDesktop?.setPendingInviteToken?.("");
			void queryClient.invalidateQueries({ queryKey: queryKeys.workspaceMembers(accepted.workspace.id, auth.token) });
		}
	}, [accepted, auth.token, queryClient]);

	return (
		<PageFrame
			title={t("workspaceInvite.title")}
			subtitle={auth.status === "signed-in" ? t("workspaceInvite.subtitleSignedIn") : t("workspaceInvite.subtitleSignedOut")}
			actions={
				accepted ? (
					<Button type="button" onClick={() => navigate({ to: "/inbox" })}>
						<ShieldCheck data-icon />
						{t("workspaceInvite.openWorkspace")}
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
								{accepted ? t("workspaceInvite.joinedTitle", { workspace: accepted.workspace.name }) : t("workspaceInvite.inviteTitle")}
							</h2>
							<p className="mt-1 text-[13px] leading-6 text-[color:var(--muted)] text-pretty">
								{accepted
									? t("workspaceInvite.joinedBody")
									: inviteBody}
							</p>
						</div>
					</div>

					<div className="mt-5 grid gap-3 rounded-[9px] bg-[color:var(--block)] p-3 shadow-[inset_0_0_0_1px_var(--line)]">
						{invitePreview.isLoading ? (
							<InviteFact label={t("workspaceInvite.inviter")} value={t("workspaceInvite.loadingInvite")} />
						) : invitePreview.data ? (
							<>
								<InviteFact
									label={t("workspaceInvite.inviter")}
									value={<InvitePerson name={inviterName} avatarUrl={invitePreview.data.invitedByAvatarUrl} login={invitePreview.data.invitedByLogin} />}
								/>
							</>
						) : (
							<InviteFact label={t("workspaceInvite.inviter")} value={t("workspaceInvite.unknownInviter")} />
						)}
						<InviteFact label={t("workspaceInvite.currentAccount")} value={currentAccountLabel} />
						{accepted ? (
							<>
								<InviteFact label={t("workspaceInvite.workspace")} value={accepted.workspace.name} />
								<InviteFact label={t("workspaceInvite.role")} value={accepted.workspace.role} />
								<InviteFact label={t("workspaceInvite.accepted")} value={<RelativeTime value={accepted.invitation.acceptedAt} />} />
							</>
						) : invitePreview.data ? (
							<>
								<InviteFact label={t("workspaceInvite.workspace")} value={invitePreview.data.workspaceName} />
								<InviteFact label={t("workspaceInvite.role")} value={invitePreview.data.role} />
								<InviteFact label={t("workspaceInvite.expires")} value={<RelativeTime value={invitePreview.data.expiresAt} />} />
							</>
						) : null}
					</div>

					{invitePreview.error ? (
						<div className="mt-4">
							<Notice tone="danger">{t("workspaceInvite.previewError")}</Notice>
						</div>
					) : null}
					{auth.status !== "signed-in" ? (
						<div className="mt-4">
							<Notice>{t("workspaceInvite.signInNotice")}</Notice>
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
							{t("workspaceInvite.inviteAccepted")}
						</div>
					) : (
						<div className="mt-5 flex justify-end">
							<Button type="button" disabled={!canAccept || acceptInvite.isPending} onClick={() => acceptInvite.mutate()}>
								<MailPlus data-icon />
								{acceptInvite.isPending ? t("workspaceInvite.joining") : t("workspaceInvite.joinWorkspace")}
							</Button>
						</div>
					)}
				</div>
			</div>
		</PageFrame>
	);
}

function InvitePerson(props: { name: string; avatarUrl?: string; login?: string }) {
	const initial = props.name.trim().slice(0, 1).toUpperCase() || "M";
	return (
		<div className="flex min-w-0 items-center gap-2">
			{props.avatarUrl ? (
				<img src={props.avatarUrl} alt="" className="size-6 shrink-0 rounded-full object-cover shadow-[0_0_0_1px_var(--line)]" />
			) : (
				<span className="grid size-6 shrink-0 place-items-center rounded-full bg-[color:var(--paper)] text-[11px] font-semibold text-[color:var(--muted-strong)] shadow-[0_0_0_1px_var(--line)]">
					{initial}
				</span>
			)}
			<span className="min-w-0 truncate text-[13px] font-medium text-[color:var(--text)]">{props.name}</span>
			{props.login ? <span className="min-w-0 truncate text-[12px] text-[color:var(--muted)]">@{props.login}</span> : null}
		</div>
	);
}

function InviteFact(props: { label: string; value: ReactNode; mono?: boolean }) {
	return (
		<div className="grid gap-1 sm:grid-cols-[120px_minmax(0,1fr)] sm:items-center">
			<div className="text-[12px] font-medium leading-5 text-[color:var(--faint)]">{props.label}</div>
			<div className={cn(props.mono ? "truncate font-mono text-[12px] text-[color:var(--muted-strong)]" : "min-w-0 truncate text-[13px] text-[color:var(--text)]")}>
				{props.value}
			</div>
		</div>
	);
}
