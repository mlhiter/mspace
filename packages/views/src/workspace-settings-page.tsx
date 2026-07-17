import { useEffect, useMemo, useState, type FormEvent, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import {
	Activity,
	ChevronDown,
	ChevronLeft,
	ChevronRight,
	CheckCircle2,
	Circle,
	Copy,
	Edit3,
	Download,
	GitCommit,
	GitPullRequest,
	KeyRound,
	ListChecks,
	MailPlus,
	ShieldCheck,
	StickyNote,
	UsersRound,
	RefreshCw,
	Save,
	ServerCog,
	Settings2,
	SquareTerminal,
	Trash2,
	X,
	type LucideIcon,
} from "lucide-react";
import {
	agentEngineMention,
	controlPlaneApi,
	getControlPlaneBaseUrl,
	queryKeys,
	type CreateWorkerInstallationInput,
	type CreateWorkspaceInvitationInput,
	type IssueListItem,
	type RuntimeRegistrationToken,
	type RuntimeTask,
	type RuntimeTaskListResult,
	type RuntimeWorker,
	type UpdateWorkspaceInput,
	type WorkspaceInvitation,
	type WorkspaceInvitationResult,
	type WorkspaceGitHubAppInstallation,
	type WorkspaceMember,
	type UpdateWorkspaceSettingsInput,
	type WorkerInstallationResult,
} from "@mspace/core";
import {
	Button,
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuLabel,
	DropdownMenuSeparator,
	DropdownMenuTrigger,
	Field,
	InlineMeta,
	Input,
	Notice,
	PageFrame,
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
	SelectValue,
	Switch,
	Textarea,
	cn,
} from "@mspace/ui";
import { useMspaceTranslation, t as translate } from "@mspace/i18n";
import { useMspaceAuth } from "./auth-context";
import { formatRelativeTime, RelativeTime } from "./time";

const ACTIVE_WORKER_MAX_AGE_MS = 45 * 1000;
const DESKTOP_PERSONAL_WORKER_CREDENTIAL_NAME = "Desktop personal worker credential";
const RUNTIME_TASKS_PAGE_SIZE = 7;

const defaultSettingsForm: UpdateWorkspaceSettingsInput = {
	autoCreateDraftPr: false,
	autoDeployTestEnvironment: false,
};

const defaultWorkerInstallationForm: CreateWorkerInstallationInput = {
	name: translate("workspaceSettings.modal.workerNamePlaceholder"),
	expiresInHours: 1,
};

const defaultInvitationForm: CreateWorkspaceInvitationInput = {
	role: "member",
	expiresInHours: 168,
};

const WORKSPACE_EMOJI_GROUPS = [
	{
		key: "workspaceEmojiCategoryWork",
		options: ["💼", "🧭", "📌", "🗂️", "🧩", "📝", "📚", "🔖"],
	},
	{
		key: "workspaceEmojiCategoryTech",
		options: ["⚙️", "🛠️", "💻", "⌨️", "🧪", "🔧", "📦", "☁️"],
	},
	{
		key: "workspaceEmojiCategoryProgress",
		options: ["🚀", "✨", "⚡", "🔥", "✅", "🎯", "🏁", "🔍"],
	},
	{
		key: "workspaceEmojiCategoryTeam",
		options: ["👥", "🤝", "💬", "🧠", "🪄", "🌱", "🛡️", "🏗️"],
	},
] as const;

export function WorkspaceSettingsPage() {
	const { t } = useMspaceTranslation();
	const queryClient = useQueryClient();
	const auth = useMspaceAuth();
	const workspaceID = auth.workspace?.id || "";
	const isSignedIn = auth.status === "signed-in" && auth.token !== "";
	const isTeamWorkspace = auth.workspace?.kind === "team";
	const runtimeEnabled = isSignedIn && workspaceID !== "";
	const settingsQueryKey = queryKeys.workspaceSettings(workspaceID, auth.token);
	const githubAppQueryKey = queryKeys.workspaceGitHubApp(workspaceID, auth.token);
	const defaultRuntimeMode = isTeamWorkspace ? "team" : "personal";
	const runtimeModeLabel = isTeamWorkspace ? t("workspaceSettings.summary.team") : t("workspaceSettings.summary.personal");
	const [runtimeTasksPage, setRuntimeTasksPage] = useState(0);
	const [selectedTaskID, setSelectedTaskID] = useState("");
	const runtimeTasksOffset = runtimeTasksPage * RUNTIME_TASKS_PAGE_SIZE;

	useEffect(() => {
		setSelectedTaskID("");
		setRuntimeTasksPage(0);
	}, [workspaceID]);

	const settingsQuery = useQuery({
		queryKey: settingsQueryKey,
		queryFn: () => controlPlaneApi.getWorkspaceSettings(auth.token, workspaceID),
		enabled: runtimeEnabled,
	});
	const githubAppQuery = useQuery({
		queryKey: githubAppQueryKey,
		queryFn: () => controlPlaneApi.getWorkspaceGitHubAppInstallation(auth.token, workspaceID),
		enabled: runtimeEnabled && isTeamWorkspace,
	});
	const tokensQuery = useQuery({
		queryKey: queryKeys.runtimeRegistrationTokens(workspaceID, auth.token),
		queryFn: () => controlPlaneApi.listRuntimeRegistrationTokens(auth.token, workspaceID),
		enabled: runtimeEnabled,
		refetchInterval: runtimeEnabled ? 15_000 : false,
	});
	const membersQuery = useQuery({
		queryKey: queryKeys.workspaceMembers(workspaceID, auth.token),
		queryFn: () => controlPlaneApi.listWorkspaceMembers(auth.token, workspaceID),
		enabled: runtimeEnabled && isTeamWorkspace,
		refetchInterval: runtimeEnabled && isTeamWorkspace ? 15_000 : false,
	});
	const invitationsQuery = useQuery({
		queryKey: queryKeys.workspaceInvitations(workspaceID, auth.token),
		queryFn: () => controlPlaneApi.listWorkspaceInvitations(auth.token, workspaceID),
		enabled: runtimeEnabled && isTeamWorkspace && (auth.workspace?.role === "owner" || auth.workspace?.role === "admin"),
		refetchInterval: runtimeEnabled && isTeamWorkspace ? 15_000 : false,
	});
	const workersQuery = useQuery({
		queryKey: queryKeys.runtimeWorkers(workspaceID, auth.token),
		queryFn: () => controlPlaneApi.listRuntimeWorkers(auth.token, workspaceID),
		enabled: runtimeEnabled,
		refetchInterval: runtimeEnabled ? 5_000 : false,
	});
	const tasksQuery = useQuery({
		queryKey: queryKeys.runtimeTasks(workspaceID, auth.token, RUNTIME_TASKS_PAGE_SIZE, runtimeTasksOffset),
		queryFn: () => controlPlaneApi.listRuntimeTasks(auth.token, workspaceID, { limit: RUNTIME_TASKS_PAGE_SIZE, offset: runtimeTasksOffset }),
		enabled: runtimeEnabled,
		refetchInterval: runtimeEnabled ? 5_000 : false,
	});
	const issuesQuery = useQuery({
		queryKey: queryKeys.workspaceIssues(workspaceID, auth.token),
		queryFn: () => controlPlaneApi.listIssues(auth.token, workspaceID),
		enabled: runtimeEnabled,
		refetchInterval: runtimeEnabled ? 15_000 : false,
	});

	const [form, setForm] = useState<UpdateWorkspaceSettingsInput>(defaultSettingsForm);
	const [workspaceName, setWorkspaceName] = useState(auth.workspace?.name || "");
	const [workspaceIcon, setWorkspaceIcon] = useState(auth.workspace?.icon || "");
	const [workspaceDescription, setWorkspaceDescription] = useState(auth.workspace?.description || "");
	const [invitationModalOpen, setInvitationModalOpen] = useState(false);
	const [invitationForm, setInvitationForm] = useState<CreateWorkspaceInvitationInput>(defaultInvitationForm);
	const [createdInvitation, setCreatedInvitation] = useState<WorkspaceInvitationResult | null>(null);
	const [workerInstallModalOpen, setWorkerInstallModalOpen] = useState(false);
	const [workerInstallationForm, setWorkerInstallationForm] = useState<CreateWorkerInstallationInput>(defaultWorkerInstallationForm);
	const [createdWorkerInstallation, setCreatedWorkerInstallation] = useState<WorkerInstallationResult | null>(null);
	const [copyState, setCopyState] = useState("");

	useEffect(() => {
		if (!settingsQuery.data) return;
		setForm({
			autoCreateDraftPr: settingsQuery.data.autoCreateDraftPr,
			autoDeployTestEnvironment: settingsQuery.data.autoDeployTestEnvironment,
		});
	}, [settingsQuery.data]);

	useEffect(() => {
		setWorkspaceName(auth.workspace?.name || "");
		setWorkspaceIcon(auth.workspace?.icon || "");
		setWorkspaceDescription(auth.workspace?.description || "");
	}, [auth.workspace?.id, auth.workspace?.name, auth.workspace?.icon, auth.workspace?.description]);

	useEffect(() => {
		const total = tasksQuery.data?.total || 0;
		if (total === 0 || runtimeTasksOffset < total) return;
		setSelectedTaskID("");
		setRuntimeTasksPage(Math.max(0, Math.ceil(total / RUNTIME_TASKS_PAGE_SIZE) - 1));
	}, [runtimeTasksOffset, tasksQuery.data?.total]);

	const saveSettings = useMutation({
		mutationFn: (input: UpdateWorkspaceSettingsInput) =>
			controlPlaneApi.updateWorkspaceSettings(auth.token, workspaceID, input),
		onSuccess: async (settings) => {
			setForm({
				autoCreateDraftPr: settings.autoCreateDraftPr,
				autoDeployTestEnvironment: settings.autoDeployTestEnvironment,
			});
			await queryClient.invalidateQueries({ queryKey: settingsQueryKey });
		},
	});
	const updateWorkspace = useMutation({
		mutationFn: (input: UpdateWorkspaceInput) => controlPlaneApi.updateWorkspace(auth.token, workspaceID, input),
		onSuccess: async (result) => {
			setWorkspaceName(result.workspace.name);
			setWorkspaceIcon(result.workspace.icon);
			setWorkspaceDescription(result.workspace.description);
			await auth.refreshAuth?.();
		},
	});
	const createInvitation = useMutation({
		mutationFn: (input: CreateWorkspaceInvitationInput) =>
			controlPlaneApi.createWorkspaceInvitation(auth.token, workspaceID, input),
		onSuccess: async (result) => {
			setCreatedInvitation(result);
			setInvitationModalOpen(false);
			setInvitationForm(defaultInvitationForm);
			await Promise.all([
				queryClient.invalidateQueries({ queryKey: queryKeys.workspaceInvitations(workspaceID, auth.token) }),
				queryClient.invalidateQueries({ queryKey: queryKeys.workspaceMembers(workspaceID, auth.token) }),
			]);
		},
	});
	const revokeInvitation = useMutation({
		mutationFn: (invitationID: string) => controlPlaneApi.revokeWorkspaceInvitation(auth.token, workspaceID, invitationID),
		onSuccess: async () => {
			await queryClient.invalidateQueries({ queryKey: queryKeys.workspaceInvitations(workspaceID, auth.token) });
		},
	});
	const createWorkerInstallation = useMutation({
		mutationFn: (input: CreateWorkerInstallationInput) =>
			controlPlaneApi.createWorkerInstallation(auth.token, workspaceID, input),
		onSuccess: async (result) => {
			setCreatedWorkerInstallation(result);
			setWorkerInstallModalOpen(false);
			setWorkerInstallationForm(defaultWorkerInstallationForm);
			await Promise.all([
				queryClient.invalidateQueries({ queryKey: queryKeys.runtimeRegistrationTokens(workspaceID, auth.token) }),
				queryClient.invalidateQueries({ queryKey: queryKeys.runtimeWorkers(workspaceID, auth.token) }),
			]);
		},
	});
	const revokeToken = useMutation({
		mutationFn: (tokenID: string) => controlPlaneApi.revokeRuntimeRegistrationToken(auth.token, workspaceID, tokenID),
		onSuccess: async () => {
			await queryClient.invalidateQueries({ queryKey: queryKeys.runtimeRegistrationTokens(workspaceID, auth.token) });
		},
	});
	const cancelTask = useMutation({
		mutationFn: (taskID: string) => controlPlaneApi.cancelRuntimeTask(auth.token, workspaceID, taskID, { reason: t("workspaceSettings.taskCancelReason") }),
		onSuccess: async (_task, taskID) => {
			await Promise.all([
				queryClient.invalidateQueries({ queryKey: ["runtime-tasks", workspaceID, auth.token] }),
				queryClient.invalidateQueries({ queryKey: queryKeys.runtimeTaskEvents(workspaceID, taskID, auth.token) }),
			]);
		},
	});

	const isDirty = Boolean(
		settingsQuery.data &&
			(form.autoCreateDraftPr !== settingsQuery.data.autoCreateDraftPr ||
				form.autoDeployTestEnvironment !== settingsQuery.data.autoDeployTestEnvironment),
	);
	const trimmedWorkspaceName = workspaceName.trim();
	const trimmedWorkspaceIcon = workspaceIcon.trim();
	const trimmedWorkspaceDescription = workspaceDescription.trim();
	const workspaceIdentityDirty = Boolean(
		auth.workspace &&
			trimmedWorkspaceName !== "" &&
			(trimmedWorkspaceName !== auth.workspace.name ||
				trimmedWorkspaceIcon !== (auth.workspace.icon || "") ||
				trimmedWorkspaceDescription !== (auth.workspace.description || "")),
	);
	const workspaceDefaultIcon = trimmedWorkspaceName.slice(0, 1).toUpperCase() || "m";
	const workers = workersQuery.data || [];
	const tokens = tokensQuery.data || [];
	const tasksPage = tasksQuery.data || emptyRuntimeTaskListResult(RUNTIME_TASKS_PAGE_SIZE, runtimeTasksOffset);
	const tasks = tasksPage.tasks || [];
	const issues = issuesQuery.data || [];
	const members = membersQuery.data || [];
	const invitations = invitationsQuery.data || [];
	const canManageWorkspace = auth.workspace?.role === "owner" || auth.workspace?.role === "admin";
	const canConnectWorker = runtimeEnabled && canManageWorkspace;
	const onlineWorkerCount = workers.filter((worker) => workerDisplayStatus(worker) === "online").length;
	const queuedTaskCount = tasksPage.statusCounts?.queued || 0;
	const runtimeError = isTeamWorkspace
		? membersQuery.error || invitationsQuery.error || tokensQuery.error || workersQuery.error || tasksQuery.error
		: tokensQuery.error || workersQuery.error || tasksQuery.error;

	function refreshRuntime() {
		const refreshes: Array<Promise<unknown>> = [tokensQuery.refetch(), workersQuery.refetch(), tasksQuery.refetch(), issuesQuery.refetch()];
		if (isTeamWorkspace) {
			refreshes.push(membersQuery.refetch(), invitationsQuery.refetch());
		}
		void Promise.all(refreshes);
	}

	function submitInvitation(event: FormEvent<HTMLFormElement>) {
		event.preventDefault();
		if (!runtimeEnabled || !isTeamWorkspace || !canManageWorkspace) return;
		createInvitation.mutate({
			role: invitationForm.role,
			expiresInHours: invitationForm.expiresInHours,
		});
	}

	function submitWorkerInstallation(event: FormEvent<HTMLFormElement>) {
		event.preventDefault();
		if (!canConnectWorker) return;
		createWorkerInstallation.mutate({
			name: workerInstallationForm.name.trim() || defaultWorkerInstallationForm.name,
			expiresInHours: workerInstallationForm.expiresInHours,
		});
	}

	function submitWorkspaceIdentity(event: FormEvent<HTMLFormElement>) {
		event.preventDefault();
		if (!runtimeEnabled || !isTeamWorkspace || !canManageWorkspace || !workspaceIdentityDirty) return;
		updateWorkspace.mutate({
			name: trimmedWorkspaceName,
			icon: trimmedWorkspaceIcon,
			description: trimmedWorkspaceDescription,
		});
	}

	async function copyWorkerInstallCommand() {
		if (!createdWorkerInstallation?.installCommand) return;
		try {
			await navigator.clipboard.writeText(createdWorkerInstallation.installCommand);
			setCopyState(t("workspaceSettings.commandCopied"));
		} catch {
			setCopyState(t("workspaceSettings.copyFailed"));
		}
	}

	async function copyCreatedInvitationLink() {
		if (!createdInvitation?.token) return;
		try {
			await navigator.clipboard.writeText(buildInviteLink(createdInvitation.token));
			setCopyState(t("workspaceSettings.copied"));
		} catch {
			setCopyState(t("workspaceSettings.copyFailed"));
		}
	}

	return (
		<PageFrame
			title={t("workspaceSettings.title")}
			subtitle={t("workspaceSettings.subtitle")}
			actions={
				isDirty || saveSettings.isPending ? (
					<Button
						type="button"
						disabled={!isDirty || saveSettings.isPending}
						onClick={() => saveSettings.mutate(form)}
					>
						<Save data-icon />
						{saveSettings.isPending ? t("workspaceSettings.saving") : t("workspaceSettings.save")}
					</Button>
				) : settingsQuery.data ? (
					<span className="inline-flex h-9 items-center gap-1.5 rounded-[7px] px-2.5 text-[12px] font-medium leading-5 text-[color:var(--muted)]">
						<CheckCircle2 data-icon className="size-4 text-[color:var(--success)]" />
						{t("workspaceSettings.saved")}
					</span>
				) : null
			}
		>
			<div className="grid gap-6">
				{settingsQuery.error ? <Notice tone="danger">{settingsQuery.error.message}</Notice> : null}
				{saveSettings.error ? <Notice tone="danger">{saveSettings.error.message}</Notice> : null}
				{updateWorkspace.error ? <Notice tone="danger">{updateWorkspace.error.message}</Notice> : null}
				{runtimeError ? <Notice tone="danger">{runtimeError.message}</Notice> : null}
				{createInvitation.error ? <Notice tone="danger">{createInvitation.error.message}</Notice> : null}
				{revokeInvitation.error ? <Notice tone="danger">{revokeInvitation.error.message}</Notice> : null}
				{createWorkerInstallation.error ? <Notice tone="danger">{createWorkerInstallation.error.message}</Notice> : null}

				{isTeamWorkspace ? (
					<SettingsSection
						title={t("workspaceSettings.section.identity")}
						description={t("workspaceSettings.section.identityDescription")}
						meta={t("workspaceSettings.summary.team")}
					>
						<form onSubmit={submitWorkspaceIdentity}>
							<SettingsRow
								icon={Edit3}
								title={t("workspaceSettings.section.workspaceName")}
								description={t("workspaceSettings.section.workspaceNameDescription")}
								control={
									<div className="w-full min-w-0 md:w-[360px]">
										<Input
											value={workspaceName}
											disabled={!runtimeEnabled || !canManageWorkspace || updateWorkspace.isPending}
											maxLength={120}
											aria-label={t("workspaceSettings.section.workspaceName")}
											onChange={(event) => setWorkspaceName(event.target.value)}
											className="w-full"
										/>
									</div>
								}
							/>
							<SettingsRow
								icon={StickyNote}
								title={t("workspaceSettings.section.workspaceIcon")}
								description={t("workspaceSettings.section.workspaceIconDescription")}
								control={
									<WorkspaceIconPicker
										value={workspaceIcon}
										defaultValue={workspaceDefaultIcon}
										disabled={!runtimeEnabled || !canManageWorkspace || updateWorkspace.isPending}
										onChange={setWorkspaceIcon}
									/>
								}
							/>
							<SettingsRow
								icon={ListChecks}
								title={t("workspaceSettings.section.workspaceDescription")}
								description={t("workspaceSettings.section.workspaceDescriptionDescription")}
								control={
									<div className="w-full min-w-0 md:w-[360px]">
										<Textarea
											value={workspaceDescription}
											disabled={!runtimeEnabled || !canManageWorkspace || updateWorkspace.isPending}
											maxLength={280}
											aria-label={t("workspaceSettings.section.workspaceDescription")}
											onChange={(event) => setWorkspaceDescription(event.target.value)}
											placeholder={t("workspaceSettings.section.workspaceDescriptionPlaceholder")}
											className="min-h-[84px] w-full resize-none"
										/>
										<div className="mt-1 text-right text-[11px] leading-4 text-[color:var(--faint)]">
											{trimmedWorkspaceDescription.length}/280
										</div>
									</div>
								}
							/>
							{canManageWorkspace ? (
								<div className="flex justify-end border-t border-[color:var(--line)] px-4 py-3">
									<Button type="submit" size="sm" disabled={!workspaceIdentityDirty || updateWorkspace.isPending}>
										<Save data-icon />
										{updateWorkspace.isPending ? t("workspaceSettings.saving") : t("workspaceSettings.save")}
									</Button>
								</div>
							) : null}
						</form>
						{!canManageWorkspace ? (
							<div className="border-t border-[color:var(--line)] p-4">
								<Notice>{t("workspaceSettings.notice.identityPermission")}</Notice>
							</div>
						) : null}
					</SettingsSection>
				) : null}

				<SettingsSection
					title={t("workspaceSettings.section.automation")}
					description={t("workspaceSettings.section.automationDescription")}
					meta={settingsQuery.isFetching ? t("workspaceSettings.section.refreshing") : t("workspaceSettings.section.workspace")}
				>
					<SettingsRow
						icon={GitCommit}
						title={t("workspaceSettings.section.commitCapture")}
						description={t("workspaceSettings.section.commitCaptureDescription")}
						control={<StatusPill>{t("workspaceSettings.section.alwaysOn")}</StatusPill>}
					/>
					<SettingsRow
						icon={GitPullRequest}
						title={t("workspaceSettings.section.draftPullRequests")}
						description={t("workspaceSettings.section.draftPullRequestsDescription")}
						control={
							<Switch
								checked={form.autoCreateDraftPr}
								disabled={!settingsQuery.data || saveSettings.isPending}
								aria-label={t("workspaceSettings.section.autoDraftPr")}
								onCheckedChange={(checked) => setForm((current) => ({ ...current, autoCreateDraftPr: checked }))}
							/>
						}
					/>
					{isTeamWorkspace ? (
						<SettingsRow
							icon={KeyRound}
							title={t("workspaceSettings.section.githubApp")}
							description={t("workspaceSettings.section.githubAppDescription")}
							control={<GitHubAppStatus installation={githubAppQuery.data} loading={githubAppQuery.isFetching} />}
						/>
					) : null}
					<SettingsRow
						icon={RefreshCw}
						title={t("workspaceSettings.section.autoTestDeploy")}
						description={t("workspaceSettings.section.autoTestDeployDescription")}
						control={
							<Switch
								checked={form.autoDeployTestEnvironment}
								disabled={!settingsQuery.data || saveSettings.isPending}
								aria-label={t("workspaceSettings.section.autoTestDeploy")}
								onCheckedChange={(checked) => setForm((current) => ({ ...current, autoDeployTestEnvironment: checked }))}
							/>
						}
					/>
				</SettingsSection>

				{isTeamWorkspace ? (
					<>
						<SettingsSection
							title={t("workspaceSettings.section.teamAccess")}
							description={t("workspaceSettings.section.teamAccessDescription")}
							meta={runtimeEnabled ? t("workspaceSettings.summary.members") : t("workspaceSettings.section.githubSignInRequired")}
							actions={
								<Button
									type="button"
									variant="secondary"
									size="sm"
									disabled={!runtimeEnabled || !canManageWorkspace}
									onClick={() => setInvitationModalOpen(true)}
								>
									<MailPlus data-icon />
									{t("workspaceSettings.section.inviteMember")}
								</Button>
							}
						>
							{runtimeEnabled ? (
								<div className="grid gap-0">
									<div className="grid gap-3 border-b border-[color:var(--line)] p-4 md:grid-cols-3">
										<RuntimeSummaryCard icon={UsersRound} label={t("workspaceSettings.summary.members")} value={`${members.length}`} meta={canManageWorkspace ? t("workspaceSettings.summary.inviteLinkEnabled") : t("workspaceSettings.summary.inviteLinkAdminOnly")} />
										<RuntimeSummaryCard icon={MailPlus} label={t("workspaceSettings.summary.openInvites")} value={`${activeInvitationCount(invitations)}`} meta={t("workspaceSettings.summary.recentInvitations", { count: invitations.length })} />
										<RuntimeSummaryCard icon={ShieldCheck} label={t("workspaceSettings.summary.yourRole")} value={auth.workspace?.role || "member"} meta={auth.workspace?.name || t("workspaceSettings.summary.selectedWorkspace")} />
									</div>
									<MemberList members={members} loading={membersQuery.isPending && runtimeEnabled} currentUserID={auth.user?.id || ""} />
									<InvitationList
										invitations={invitations}
										loading={invitationsQuery.isPending && runtimeEnabled && canManageWorkspace}
										canManage={canManageWorkspace}
										disabled={revokeInvitation.isPending}
										onRevoke={(invitationID) => revokeInvitation.mutate(invitationID)}
									/>
								</div>
							) : (
								<div className="p-4">
									<Notice>{t("workspaceSettings.notice.signInForTeam")}</Notice>
								</div>
							)}
						</SettingsSection>
					</>
				) : null}
				<SettingsSection
					title={t("workspaceSettings.section.runtime")}
					description={t("workspaceSettings.section.runtimeDescription")}
					meta={runtimeEnabled ? auth.workspace?.name : t("workspaceSettings.section.githubSignInRequired")}
					actions={
						<Button type="button" variant="secondary" size="sm" disabled={!runtimeEnabled} onClick={refreshRuntime}>
							<RefreshCw data-icon />
							{t("workspaceSettings.section.refresh")}
						</Button>
					}
				>
					{runtimeEnabled ? (
						<div className="grid gap-0">
							<div className="grid gap-3 border-b border-[color:var(--line)] p-4 md:grid-cols-3">
								<RuntimeSummaryCard icon={SquareTerminal} label={t("workspaceSettings.summary.runtimeMode")} value={runtimeModeLabel} meta={t("workspaceSettings.summary.serverOwnedQueue")} />
								<RuntimeSummaryCard icon={ServerCog} label={t("workspaceSettings.summary.workers")} value={t("workspaceSettings.summary.online", { count: onlineWorkerCount })} meta={t("workspaceSettings.summary.registered", { count: workers.length })} />
								<RuntimeSummaryCard icon={ListChecks} label={t("workspaceSettings.summary.taskQueue")} value={t("workspaceSettings.summary.queued", { count: queuedTaskCount })} meta={t("workspaceSettings.summary.recentTasks", { count: tasksPage.total })} />
							</div>
							<SettingsRow
								icon={Settings2}
								title={t("workspaceSettings.section.controlPlane")}
								description={t("workspaceSettings.section.controlPlaneDescription")}
								control={<StatusPill>{auth.workspace?.kind || "workspace"}</StatusPill>}
							/>
							<SettingsRow
								icon={ServerCog}
								title={t("workspaceSettings.section.connectWorker")}
								description={t("workspaceSettings.section.connectWorkerDescription")}
								control={
									<Button
										type="button"
										variant="secondary"
										size="sm"
										disabled={!canConnectWorker}
										onClick={() => setWorkerInstallModalOpen(true)}
									>
										<Download data-icon />
										{t("workspaceSettings.section.connectEnvironment")}
									</Button>
								}
							/>
							{!canManageWorkspace ? (
								<div className="border-t border-[color:var(--line)] px-4 py-3">
									<Notice>{t("workspaceSettings.notice.workerPermission")}</Notice>
								</div>
							) : null}
						</div>
					) : (
						<div className="p-4">
							<Notice>
								{t("workspaceSettings.notice.signInForRuntime")}
							</Notice>
						</div>
					)}
				</SettingsSection>

				<RuntimePanel
					title={t("workspaceSettings.section.advancedCredentials")}
					description={t("workspaceSettings.section.advancedCredentialsDescription")}
				>
					<RegistrationTokenList
						tokens={tokens}
						loading={tokensQuery.isPending && runtimeEnabled}
						disabled={!runtimeEnabled || revokeToken.isPending}
						onRevoke={(tokenID) => revokeToken.mutate(tokenID)}
					/>
				</RuntimePanel>

				<RuntimePanel
					title={t("workspaceSettings.section.workers")}
					description={t("workspaceSettings.section.workersDescription")}
				>
					<WorkerList workers={workers} loading={workersQuery.isPending && runtimeEnabled} />
				</RuntimePanel>

				<RuntimePanel
					title={t("workspaceSettings.section.taskQueue")}
					description={t("workspaceSettings.section.taskQueueDescription")}
				>
					<TaskList
						tasks={tasks}
						page={runtimeTasksPage}
						pageSize={RUNTIME_TASKS_PAGE_SIZE}
						total={tasksPage.total}
						workers={workers}
						issues={issues}
						loading={tasksQuery.isPending && runtimeEnabled}
						token={auth.token}
						workspaceID={workspaceID}
						selectedTaskID={selectedTaskID}
						onSelectTask={setSelectedTaskID}
						onPageChange={(page) => {
							setSelectedTaskID("");
							setRuntimeTasksPage(page);
						}}
						cancellingTaskID={cancelTask.variables || ""}
						onCancelTask={(taskID) => cancelTask.mutate(taskID)}
					/>
				</RuntimePanel>
				{cancelTask.error ? <Notice tone="danger">{cancelTask.error.message}</Notice> : null}
			</div>

			{invitationModalOpen ? (
				<Modal title={t("workspaceSettings.modal.inviteTitle")} description={t("workspaceSettings.modal.inviteDescription")} onClose={() => setInvitationModalOpen(false)}>
					<form className="grid gap-4" onSubmit={submitInvitation}>
						{createInvitation.error ? <Notice tone="danger">{createInvitation.error.message}</Notice> : null}
						<div className="grid gap-3 md:grid-cols-2">
							<Field label={t("workspaceSettings.modal.role")}>
								<Select
									value={invitationForm.role}
									onValueChange={(value) => setInvitationForm({ ...invitationForm, role: value === "admin" ? "admin" : "member" })}
								>
									<SelectTrigger>
										<SelectValue placeholder={t("workspaceSettings.modal.workspaceRole")} />
									</SelectTrigger>
									<SelectContent>
										<SelectItem value="member">{t("workspaceSettings.modal.member")}</SelectItem>
										<SelectItem value="admin">{t("workspaceSettings.modal.admin")}</SelectItem>
									</SelectContent>
								</Select>
							</Field>
							<Field label={t("workspaceSettings.modal.expires")}>
								<Select
									value={String(invitationForm.expiresInHours)}
									onValueChange={(value) => setInvitationForm({ ...invitationForm, expiresInHours: Number(value) })}
								>
									<SelectTrigger>
										<SelectValue placeholder={t("workspaceSettings.modal.inviteExpiry")} />
									</SelectTrigger>
									<SelectContent>
										<SelectItem value="12">{t("workspaceSettings.modal.hours", { count: 12, suffix: "s" })}</SelectItem>
										<SelectItem value="24">{t("workspaceSettings.modal.hours", { count: 24, suffix: "s" })}</SelectItem>
										<SelectItem value="168">{t("workspaceSettings.modal.days", { count: 7 })}</SelectItem>
										<SelectItem value="720">{t("workspaceSettings.modal.days", { count: 30 })}</SelectItem>
									</SelectContent>
								</Select>
							</Field>
						</div>
						<div className="flex justify-end gap-2 border-t border-[color:var(--line)] pt-4">
							<Button type="button" variant="secondary" onClick={() => setInvitationModalOpen(false)}>
								{t("common.cancel")}
							</Button>
							<Button type="submit" disabled={!runtimeEnabled || !isTeamWorkspace || !canManageWorkspace || createInvitation.isPending}>
								<MailPlus data-icon />
								{createInvitation.isPending ? t("common.creating") : t("workspaceSettings.modal.createInvite")}
							</Button>
						</div>
					</form>
				</Modal>
			) : null}

			{createdInvitation ? (
				<Modal title={t("workspaceSettings.modal.inviteLinkCreatedTitle")} description={t("workspaceSettings.modal.inviteLinkCreatedDescription")} onClose={() => {
					setCreatedInvitation(null);
					setCopyState("");
				}}>
					<div className="grid min-w-0 gap-3">
						<div className="flex min-w-0 max-w-full overflow-hidden rounded-[9px] bg-[color:var(--block)] shadow-[inset_0_0_0_1px_var(--line)]">
							<div className="min-w-0 flex-1 whitespace-pre-wrap break-all px-3 py-3 font-mono text-[12px] leading-6 text-[color:var(--muted-strong)]">
								{buildInviteLink(createdInvitation.token)}
							</div>
							<Button
								type="button"
								variant="ghost"
								className="h-auto min-w-[88px] self-stretch rounded-none border-l border-[color:var(--line)] px-3 text-[12px] text-[color:var(--text)] hover:bg-[color:var(--hover)]"
								onClick={copyCreatedInvitationLink}
							>
								<Copy data-icon />
								{copyState || t("workspaceSettings.modal.copyInviteLinkShort")}
							</Button>
						</div>
						<div className="text-[12px] leading-5 text-[color:var(--muted)]">
							{t("workspaceSettings.modal.inviteSummary", {
								role: createdInvitation.invitation.role,
							})}{" "}
							<RelativeTime value={createdInvitation.invitation.expiresAt} />.
						</div>
					</div>
				</Modal>
			) : null}

			{workerInstallModalOpen ? (
				<Modal title={t("workspaceSettings.modal.workerInstallTitle")} description={t("workspaceSettings.modal.workerInstallDescription")} onClose={() => setWorkerInstallModalOpen(false)}>
					<form className="grid gap-4" onSubmit={submitWorkerInstallation}>
						{createWorkerInstallation.error ? <Notice tone="danger">{createWorkerInstallation.error.message}</Notice> : null}
						<Field label={t("workspaceSettings.modal.name")} hint={t("workspaceSettings.modal.tokenNameHint")}>
							<Input
								value={workerInstallationForm.name}
								onChange={(event) => setWorkerInstallationForm({ ...workerInstallationForm, name: event.target.value })}
								placeholder={t("workspaceSettings.modal.workerNamePlaceholder")}
							/>
						</Field>
						<Field label={t("workspaceSettings.modal.expires")}>
							<Select
								value={String(workerInstallationForm.expiresInHours)}
								onValueChange={(value) => setWorkerInstallationForm({ ...workerInstallationForm, expiresInHours: Number(value) })}
							>
								<SelectTrigger>
									<SelectValue placeholder={t("workspaceSettings.modal.joinCodeExpiry")} />
								</SelectTrigger>
								<SelectContent>
									<SelectItem value="1">{t("workspaceSettings.modal.hours", { count: 1, suffix: "" })}</SelectItem>
									<SelectItem value="12">{t("workspaceSettings.modal.hours", { count: 12, suffix: "s" })}</SelectItem>
									<SelectItem value="24">{t("workspaceSettings.modal.hours", { count: 24, suffix: "s" })}</SelectItem>
								</SelectContent>
							</Select>
						</Field>
						<div className="flex justify-end gap-2 border-t border-[color:var(--line)] pt-4">
							<Button type="button" variant="secondary" onClick={() => setWorkerInstallModalOpen(false)}>
								{t("common.cancel")}
							</Button>
							<Button type="submit" disabled={!canConnectWorker || createWorkerInstallation.isPending}>
								<Download data-icon />
								{createWorkerInstallation.isPending ? t("common.creating") : t("workspaceSettings.modal.createInstallCommand")}
							</Button>
						</div>
					</form>
				</Modal>
			) : null}

			{createdWorkerInstallation ? (
				<Modal title={t("workspaceSettings.modal.workerInstallCreatedTitle")} description={t("workspaceSettings.modal.workerInstallCreatedDescription")} onClose={() => {
					setCreatedWorkerInstallation(null);
					setCopyState("");
				}}>
					<div className="grid gap-4">
						<div className="text-[12px] leading-5 text-[color:var(--muted)]">
							{t("workspaceSettings.modal.installSummary", {
								name: createdWorkerInstallation.workerName,
								mode: createdWorkerInstallation.runtimeMode,
								prefix: createdWorkerInstallation.credentialPrefix,
							})}{" "}
							<RelativeTime value={createdWorkerInstallation.expiresAt} />.
						</div>
						<div className="grid gap-2">
							<div className="text-[12px] font-medium leading-5 text-[color:var(--muted)]">{t("workspaceSettings.modal.installCommand")}</div>
							<div className="overflow-auto rounded-[9px] bg-[color:var(--code-bg)] px-3 py-3 font-mono text-[12px] leading-6 text-[color:var(--code-text)]">
								{createdWorkerInstallation.installCommand}
							</div>
							<div className="text-[12px] leading-5 text-[color:var(--muted)]">
								{t("workspaceSettings.modal.installCommandDescription")}
							</div>
						</div>
						<div className="flex justify-end gap-2 border-t border-[color:var(--line)] pt-4">
							<Button type="button" onClick={copyWorkerInstallCommand}>
								<SquareTerminal data-icon />
								{copyState || t("workspaceSettings.modal.copyInstallCommand")}
							</Button>
							<Button type="button" variant="secondary" onClick={() => {
								setCreatedWorkerInstallation(null);
								setCopyState("");
							}}>
								{t("workspaceSettings.modal.done")}
							</Button>
						</div>
					</div>
				</Modal>
			) : null}

		</PageFrame>
	);
}

function SettingsSection(props: {
	title: string;
	description: string;
	meta?: string;
	actions?: ReactNode;
	children: ReactNode;
}) {
	return (
		<section className="grid gap-2">
			<div className="flex min-w-0 items-end justify-between gap-4 px-0.5">
				<div className="min-w-0">
					<h2 className="text-[14px] font-semibold leading-5 text-[color:var(--text)]">{props.title}</h2>
					<p className="mt-1 max-w-[72ch] text-[12px] leading-5 text-[color:var(--muted)] text-pretty">{props.description}</p>
				</div>
				<div className="flex shrink-0 items-center gap-2">
					{props.meta ? <span className="text-[12px] leading-5 text-[color:var(--faint)]">{props.meta}</span> : null}
					{props.actions}
				</div>
			</div>
			<div className="overflow-hidden rounded-[10px] bg-[color:var(--surface)] shadow-[inset_0_0_0_1px_var(--line)]">
				{props.children}
			</div>
		</section>
	);
}

function SettingsRow(props: {
	icon: LucideIcon;
	title: string;
	description: string;
	control: ReactNode;
}) {
	const Icon = props.icon;
	return (
		<div className="grid min-w-0 gap-3 border-b border-[color:var(--line)] px-4 py-3.5 last:border-b-0 md:grid-cols-[32px_minmax(0,1fr)_auto] md:items-center">
			<span className="grid size-8 place-items-center rounded-[8px] bg-[color:var(--block)] text-[color:var(--muted)] shadow-[inset_0_0_0_1px_var(--line)]">
				<Icon data-icon />
			</span>
			<span className="min-w-0">
				<span className="block text-[13px] font-medium leading-5 text-[color:var(--text)]">{props.title}</span>
				<span className="mt-1 block max-w-[76ch] text-[12px] leading-5 text-[color:var(--muted)] text-pretty">{props.description}</span>
			</span>
			<div className="justify-self-start md:justify-self-end">{props.control}</div>
		</div>
	);
}

function WorkspaceIconPicker(props: {
	value: string;
	defaultValue: string;
	disabled: boolean;
	onChange: (value: string) => void;
}) {
	const { t } = useMspaceTranslation();
	const [open, setOpen] = useState(false);
	const trimmedValue = props.value.trim();
	const selectedLabel = trimmedValue || props.defaultValue;

	return (
		<DropdownMenu open={open} onOpenChange={setOpen}>
			<DropdownMenuTrigger asChild>
				<button
					type="button"
					disabled={props.disabled}
					className="flex h-9 w-full min-w-0 items-center justify-between gap-3 rounded-[8px] bg-[color:var(--paper)] px-2.5 text-left shadow-[inset_0_0_0_1px_var(--line)] outline-none transition-[background-color,box-shadow] duration-150 ease-out hover:bg-[color:var(--hover)] focus-visible:shadow-[0_0_0_1px_var(--accent),0_0_0_3px_var(--accent-soft)] disabled:pointer-events-none disabled:opacity-50 md:w-[360px]"
					aria-label={t("workspaceSettings.section.workspaceIcon")}
				>
					<span className="flex min-w-0 items-center gap-2.5">
						<span className="grid size-7 shrink-0 place-items-center rounded-[7px] bg-[color:var(--surface)] px-1 text-[17px] leading-none shadow-[inset_0_0_0_1px_var(--line)]">
							<span className="max-w-full truncate">{selectedLabel}</span>
						</span>
						<span className="truncate text-[13px] font-medium leading-5 text-[color:var(--text)]">
							{trimmedValue
								? t("workspaceSettings.section.workspaceIconSelected", { mark: selectedLabel })
								: t("workspaceSettings.section.workspaceIconDefaultOption", { mark: selectedLabel })}
						</span>
					</span>
					<ChevronDown data-icon className="shrink-0 text-[color:var(--muted)]" />
				</button>
			</DropdownMenuTrigger>
			<DropdownMenuContent align="end" className="w-[320px] p-2">
				<div className="flex items-center justify-between gap-2 px-1 pb-1">
					<DropdownMenuLabel className="px-0 py-0">
						{t("workspaceSettings.section.workspaceEmojiPicker")}
					</DropdownMenuLabel>
					<button
						type="button"
						disabled={props.disabled}
						onClick={() => {
							props.onChange("");
							setOpen(false);
						}}
						className={cn(
							"inline-flex h-7 items-center gap-1.5 rounded-[7px] px-2 text-[12px] font-medium leading-4 text-[color:var(--muted-strong)] outline-none transition-[background-color,box-shadow,color] duration-150 ease-out hover:bg-[color:var(--hover)] hover:text-[color:var(--text)] focus-visible:shadow-[0_0_0_1px_var(--accent),0_0_0_3px_var(--accent-soft)] disabled:pointer-events-none disabled:opacity-50",
							!trimmedValue && "bg-[color:var(--hover)] text-[color:var(--text)]",
						)}
					>
						<span className="grid size-5 place-items-center rounded-[6px] bg-[color:var(--surface)] text-[11px] font-semibold leading-none shadow-[inset_0_0_0_1px_var(--line)]">
							{props.defaultValue}
						</span>
						{t("workspaceSettings.section.workspaceIconUseDefault")}
					</button>
				</div>
				{WORKSPACE_EMOJI_GROUPS.map((group, index) => (
					<div key={group.key}>
						{index > 0 ? <DropdownMenuSeparator /> : null}
						<DropdownMenuLabel>{t(`workspaceSettings.section.${group.key}`)}</DropdownMenuLabel>
						<div className="grid grid-cols-8 gap-1 px-1 pb-1">
							{group.options.map((emoji) => (
								<button
									key={emoji}
									type="button"
									disabled={props.disabled}
									aria-pressed={trimmedValue === emoji}
									aria-label={t("workspaceSettings.section.workspaceIconOption", { mark: emoji })}
									title={t("workspaceSettings.section.workspaceIconOption", { mark: emoji })}
									onClick={() => {
										props.onChange(emoji);
										setOpen(false);
									}}
									className={cn(
										"grid size-8 place-items-center rounded-[7px] text-[18px] leading-none outline-none transition-[background-color,box-shadow,transform] duration-150 ease-out hover:bg-[color:var(--hover)] focus-visible:shadow-[0_0_0_1px_var(--accent),0_0_0_3px_var(--accent-soft)] active:scale-95 disabled:pointer-events-none disabled:opacity-50",
										trimmedValue === emoji && "bg-[color:var(--hover)] shadow-[inset_0_0_0_1px_var(--accent)]",
									)}
								>
									<span aria-hidden="true">{emoji}</span>
								</button>
							))}
						</div>
					</div>
				))}
			</DropdownMenuContent>
		</DropdownMenu>
	);
}

function RuntimePanel(props: {
	title: string;
	description: string;
	actions?: ReactNode;
	children: ReactNode;
}) {
	return (
		<section className="grid gap-2">
			<div className="flex min-w-0 items-end justify-between gap-4 px-0.5">
				<div className="min-w-0">
					<h2 className="text-[14px] font-semibold leading-5 text-[color:var(--text)]">{props.title}</h2>
					<p className="mt-1 max-w-[72ch] text-[12px] leading-5 text-[color:var(--muted)] text-pretty">{props.description}</p>
				</div>
				{props.actions ? <div className="shrink-0">{props.actions}</div> : null}
			</div>
			<div className="overflow-hidden rounded-[10px] bg-[color:var(--surface)] shadow-[inset_0_0_0_1px_var(--line)]">
				{props.children}
			</div>
		</section>
	);
}

function RuntimeSummaryCard(props: { icon: LucideIcon; label: string; value: string; meta: string }) {
	const Icon = props.icon;
	return (
		<div className="min-w-0 rounded-[9px] bg-[color:var(--block)] p-3 shadow-[inset_0_0_0_1px_var(--line)]">
			<div className="flex items-center gap-2 text-[12px] font-medium leading-5 text-[color:var(--muted)]">
				<Icon data-icon />
				{props.label}
			</div>
			<div className="mt-2 truncate text-[18px] font-semibold leading-6 text-[color:var(--text)]">{props.value}</div>
			<div className="mt-1 truncate text-[12px] leading-5 text-[color:var(--muted)]">{props.meta}</div>
		</div>
	);
}

function emptyRuntimeTaskListResult(limit: number, offset: number): RuntimeTaskListResult {
	return {
		tasks: [],
		total: 0,
		limit,
		offset,
		statusCounts: {},
	};
}

function MemberList(props: { members: WorkspaceMember[]; loading: boolean; currentUserID: string }) {
	const { t } = useMspaceTranslation();

	if (props.loading) return <LoadingBlock>{t("workspaceSettings.list.loadingMembers")}</LoadingBlock>;
	if (props.members.length === 0) {
		return <EmptyRuntimeBlock icon={UsersRound} title={t("workspaceSettings.list.noMembersTitle")} body={t("workspaceSettings.list.noMembersBody")} />;
	}
	return (
		<div>
			<TableHeader columns="grid-cols-[minmax(190px,1.1fr)_150px_minmax(190px,1fr)_150px]">
				<span>{t("workspaceSettings.list.member")}</span>
				<span>{t("workspaceSettings.list.role")}</span>
				<span>{t("workspaceSettings.list.account")}</span>
				<span>{t("workspaceSettings.list.joined")}</span>
			</TableHeader>
			<div className="divide-y divide-[color:var(--line)]">
				{props.members.map((member) => (
					<div key={member.id} className="grid grid-cols-[minmax(190px,1.1fr)_150px_minmax(190px,1fr)_150px] items-center gap-4 px-4 py-3">
						<div className="flex min-w-0 items-center gap-2.5">
							<MemberAvatar member={member} />
							<div className="min-w-0">
								<div className="truncate text-[13px] font-medium text-[color:var(--text)]">
									{member.name || member.email || t("workspaceSettings.list.mspaceUser")}
									{member.userId === props.currentUserID ? <span className="ml-1 text-[12px] font-normal text-[color:var(--muted)]">({t("workspaceSettings.list.you")})</span> : null}
								</div>
								<div className="truncate text-[12px] leading-5 text-[color:var(--muted)]">{member.identityLogin || member.email || t("workspaceSettings.list.noPublicEmail")}</div>
							</div>
						</div>
						<RolePill role={member.role} />
						<span className="truncate font-mono text-[12px] text-[color:var(--muted)]">{member.identityLogin || t("workspaceSettings.list.notLinked")}</span>
						<span className="text-[12px] leading-5 text-[color:var(--muted)]"><RelativeTime value={member.createdAt} /></span>
					</div>
				))}
			</div>
		</div>
	);
}

function InvitationList(props: {
	invitations: WorkspaceInvitation[];
	loading: boolean;
	canManage: boolean;
	disabled: boolean;
	onRevoke: (invitationID: string) => void;
}) {
	const { t } = useMspaceTranslation();

	if (!props.canManage) {
		return (
			<div className="border-t border-[color:var(--line)] p-4">
				<Notice>{t("workspaceSettings.notice.invitePermission")}</Notice>
			</div>
		);
	}
	if (props.loading) return <LoadingBlock>{t("workspaceSettings.list.loadingInvitations")}</LoadingBlock>;
	if (props.invitations.length === 0) {
		return <EmptyRuntimeBlock icon={MailPlus} title={t("workspaceSettings.list.noInvitationsTitle")} body={t("workspaceSettings.list.noInvitationsBody")} />;
	}
	return (
		<div className="border-t border-[color:var(--line)]">
			<TableHeader columns="grid-cols-[minmax(190px,1fr)_120px_150px_130px_96px]">
				<span>{t("workspaceSettings.list.invitation")}</span>
				<span>{t("workspaceSettings.list.role")}</span>
				<span>{t("workspaceSettings.list.expires")}</span>
				<span>{t("workspaceSettings.list.status")}</span>
				<span className="text-right">{t("workspaceSettings.list.actions")}</span>
			</TableHeader>
			<div className="divide-y divide-[color:var(--line)]">
				{props.invitations.map((invitation) => {
					const status = invitationStatus(invitation);
					return (
						<div key={invitation.id} className="grid grid-cols-[minmax(190px,1fr)_120px_150px_130px_96px] items-center gap-4 px-4 py-3 text-[13px]">
							<div className="min-w-0">
								<div className="truncate font-medium text-[color:var(--text)]">{t("workspaceSettings.list.openInviteLink")}</div>
							</div>
							<RolePill role={invitation.role} />
							<span className="text-[12px] text-[color:var(--muted)]"><RelativeTime value={invitation.expiresAt} /></span>
							<StatusPill>{status}</StatusPill>
							<div className="flex justify-end">
								<Button
									type="button"
									variant="ghost"
									size="icon"
									aria-label={t("workspaceSettings.list.revokeInvite", { name: t("workspaceSettings.list.openInviteLink") })}
									disabled={props.disabled || status !== "pending"}
									onClick={() => props.onRevoke(invitation.id)}
								>
									<Trash2 data-icon />
								</Button>
							</div>
						</div>
					);
				})}
			</div>
		</div>
	);
}

function RegistrationTokenList(props: {
	tokens: RuntimeRegistrationToken[];
	loading: boolean;
	disabled: boolean;
	onRevoke: (tokenID: string) => void;
}) {
	const { t } = useMspaceTranslation();
	const currentTokens = props.tokens.filter((token) => tokenStatusValue(token) === "active");
	const historyTokens = props.tokens.filter((token) => tokenStatusValue(token) !== "active");

	if (props.loading) return <LoadingBlock>{t("workspaceSettings.list.loadingCredentials")}</LoadingBlock>;
	if (props.tokens.length === 0) {
		return <EmptyRuntimeBlock icon={KeyRound} title={t("workspaceSettings.list.noCredentialsTitle")} body={t("workspaceSettings.list.noCredentialsBody")} />;
	}
	return (
		<div>
			<CredentialTable tokens={currentTokens} disabled={props.disabled} onRevoke={props.onRevoke} />
			{currentTokens.length === 0 ? (
				<div className="border-t border-[color:var(--line)] px-4 py-4">
					<Notice>{t("workspaceSettings.list.noActiveCredentialsBody")}</Notice>
				</div>
			) : null}
			{historyTokens.length > 0 ? (
				<details className="border-t border-[color:var(--line)] bg-[color:var(--block)]">
					<summary className="flex cursor-pointer list-none items-center justify-between gap-3 px-4 py-3 text-[12px] font-medium leading-5 text-[color:var(--muted)] marker:hidden">
						<span>{t("workspaceSettings.list.historyCredentials", { count: historyTokens.length })}</span>
						<span className="text-[color:var(--faint)]">{t("workspaceSettings.list.historyCredentialsHint")}</span>
					</summary>
					<CredentialTable tokens={historyTokens} disabled={props.disabled} onRevoke={props.onRevoke} historical />
				</details>
			) : null}
		</div>
	);
}

function CredentialTable(props: {
	tokens: RuntimeRegistrationToken[];
	disabled: boolean;
	historical?: boolean;
	onRevoke: (tokenID: string) => void;
}) {
	const { t } = useMspaceTranslation();
	if (props.tokens.length === 0) return null;
	return (
		<div>
			<TableHeader columns="grid-cols-[minmax(190px,1.3fr)_120px_150px_150px_120px_96px]">
				<span>{t("workspaceSettings.list.name")}</span>
				<span>{t("workspaceSettings.list.prefix")}</span>
				<span>{t("workspaceSettings.list.expires")}</span>
				<span>{t("workspaceSettings.list.lastUsed")}</span>
				<span>{t("workspaceSettings.list.status")}</span>
				<span className="text-right">{t("workspaceSettings.list.actions")}</span>
			</TableHeader>
			<div className="divide-y divide-[color:var(--line)]">
				{props.tokens.map((token) => {
					const status = tokenStatusValue(token);
					const canRevoke = status === "active" && !props.historical;
					return (
						<div key={token.id} className="grid grid-cols-[minmax(190px,1.3fr)_120px_150px_150px_120px_96px] items-center gap-4 px-4 py-3 text-[13px]">
							<div className="min-w-0">
								<div className="flex min-w-0 items-center gap-2">
									<span className="truncate font-medium text-[color:var(--text)]">{credentialDisplayName(token)}</span>
									{isDesktopPersonalWorkerCredential(token) ? <StatusPill>{t("workspaceSettings.list.automaticCredential")}</StatusPill> : null}
								</div>
								<InlineMeta icon={KeyRound}>{credentialMeta(token)}</InlineMeta>
							</div>
							<span className="truncate font-mono text-[12px] text-[color:var(--muted)]">{token.tokenPrefix}</span>
							<span className="text-[12px] text-[color:var(--muted)]"><RelativeTime value={token.expiresAt} /></span>
							<span className="text-[12px] text-[color:var(--muted)]">{token.lastUsedAt ? <RelativeTime value={token.lastUsedAt} /> : t("workspaceSettings.list.never")}</span>
							<RuntimeStatusPill status={status} />
							<div className="flex justify-end">
								<Button
									type="button"
									variant="ghost"
									size="icon"
									aria-label={t("workspaceSettings.list.revokeCredential", { name: credentialDisplayName(token) })}
									disabled={props.disabled || !canRevoke}
									onClick={() => props.onRevoke(token.id)}
								>
									<Trash2 data-icon />
								</Button>
							</div>
						</div>
					);
				})}
			</div>
		</div>
	);
}

function WorkerList(props: { workers: RuntimeWorker[]; loading: boolean }) {
	const { t } = useMspaceTranslation();

	if (props.loading) return <LoadingBlock>{t("workspaceSettings.list.loadingWorkers")}</LoadingBlock>;
	if (props.workers.length === 0) {
		return <EmptyRuntimeBlock icon={ServerCog} title={t("workspaceSettings.list.noWorkersTitle")} body={t("workspaceSettings.list.noWorkersBody")} />;
	}
	return (
		<div>
			<TableHeader columns="grid-cols-[minmax(170px,1.1fr)_150px_120px_minmax(220px,1.2fr)_150px]">
				<span>{t("workspaceSettings.list.worker")}</span>
				<span>{t("workspaceSettings.list.mode")}</span>
				<span>{t("workspaceSettings.list.load")}</span>
				<span>{t("workspaceSettings.list.capabilities")}</span>
				<span>{t("workspaceSettings.list.lastSeen")}</span>
			</TableHeader>
			<div className="divide-y divide-[color:var(--line)]">
				{props.workers.map((worker) => (
					<div key={worker.id} className="grid grid-cols-[minmax(170px,1.1fr)_150px_120px_minmax(220px,1.2fr)_150px] items-center gap-4 px-4 py-3">
						<div className="min-w-0">
							<div className="truncate text-[13px] font-medium text-[color:var(--text)]">{worker.name}</div>
							<div className="mt-1 truncate text-[12px] leading-5 text-[color:var(--muted)]">{worker.version || t("workspaceSettings.list.versionNotReported")}</div>
						</div>
						<div className="flex min-w-0 flex-wrap items-center gap-1.5">
							<RuntimeStatusPill status={workerDisplayStatus(worker)} />
							<StatusPill>{worker.mode}</StatusPill>
						</div>
						<span className="font-mono text-[12px] tabular-nums text-[color:var(--muted)]">{worker.currentLoad}</span>
						<span className="truncate font-mono text-[12px] leading-5 text-[color:var(--muted)]">{jsonSummary(worker.capabilities)}</span>
						<span className="text-[12px] leading-5 text-[color:var(--muted)]"><RelativeTime value={worker.lastSeenAt} /></span>
					</div>
				))}
			</div>
		</div>
	);
}

function TaskList(props: {
	tasks: RuntimeTask[];
	page: number;
	pageSize: number;
	total: number;
	workers: RuntimeWorker[];
	issues: IssueListItem[];
	loading: boolean;
	token: string;
	workspaceID: string;
	selectedTaskID: string;
	onSelectTask: (taskID: string) => void;
	onPageChange: (page: number) => void;
	cancellingTaskID?: string;
	onCancelTask: (taskID: string) => void;
}) {
	const { t } = useMspaceTranslation();
	const workerByID = useMemo(() => new Map(props.workers.map((worker) => [worker.id, worker])), [props.workers]);
	const issueTitleByID = useMemo(() => new Map(props.issues.map((issue) => [issue.id, issue.title])), [props.issues]);
	const totalPages = Math.max(1, Math.ceil(props.total / props.pageSize));
	const clampedPage = Math.min(props.page, totalPages - 1);
	if (props.loading) return <LoadingBlock>{t("workspaceSettings.list.loadingTasks")}</LoadingBlock>;
	if (props.tasks.length === 0) {
		return <EmptyRuntimeBlock icon={ListChecks} title={t("workspaceSettings.list.noTasksTitle")} body={t("workspaceSettings.list.noTasksBody")} />;
	}
	return (
		<div>
			<TableHeader columns="grid-cols-[minmax(220px,1.25fr)_minmax(190px,1fr)_96px_minmax(160px,0.9fr)_130px_100px]">
				<span>{t("workspaceSettings.list.task")}</span>
				<span>{t("workspaceSettings.list.issue")}</span>
				<span>{t("workspaceSettings.list.status")}</span>
				<span>{t("workspaceSettings.list.worker")}</span>
				<span>{t("workspaceSettings.list.updated")}</span>
				<span>{t("workspaceSettings.list.action")}</span>
			</TableHeader>
			<div className="divide-y divide-[color:var(--line)]">
				{props.tasks.map((task) => {
					const worker = task.claimedByWorkerId ? workerByID.get(task.claimedByWorkerId) : undefined;
					const selected = props.selectedTaskID === task.id;
					const cancellable = ["queued", "claimed", "running"].includes(task.status);
					const display = runtimeTaskDisplay(task, issueTitleByID.get(task.issueId) || "");
					return (
						<div key={task.id}>
							<div className="grid w-full grid-cols-[minmax(220px,1.25fr)_minmax(190px,1fr)_96px_minmax(160px,0.9fr)_130px_100px] items-center gap-4 px-4 py-3 transition-colors hover:bg-[color:var(--hover)]">
								<button
									type="button"
									className="min-w-0 text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[color:var(--focus)]"
									aria-expanded={selected}
									onClick={() => props.onSelectTask(selected ? "" : task.id)}
								>
									<div className="min-w-0">
										<div className="flex min-w-0 items-center gap-2">
											{selected ? <ChevronDown data-icon className="shrink-0 text-[color:var(--faint)]" /> : <ChevronRight data-icon className="shrink-0 text-[color:var(--faint)]" />}
											<span className="truncate text-[13px] font-medium text-[color:var(--text)]">{display.title}</span>
											<StatusPill>{task.runtimeMode}</StatusPill>
										</div>
										<div className="mt-1 truncate pl-6 text-[12px] leading-5 text-[color:var(--muted)]">
											{display.subtitle}
										</div>
									</div>
								</button>
								<RuntimeTaskIssueLink task={task} display={display} />
								<RuntimeStatusPill status={task.status} />
								<span className="truncate text-[12px] leading-5 text-[color:var(--muted)]">{worker?.name || task.claimedByWorkerId || t("workspaceSettings.list.unclaimed")}</span>
								<span className="text-[12px] leading-5 text-[color:var(--muted)]"><RelativeTime value={task.updatedAt} /></span>
								{cancellable ? (
									<Button type="button" variant="ghost" size="sm" disabled={props.cancellingTaskID === task.id} onClick={() => props.onCancelTask(task.id)}>
										<X data-icon />
										{props.cancellingTaskID === task.id ? t("workspaceSettings.list.cancelling") : t("workspaceSettings.list.cancel")}
									</Button>
								) : (
									<Button type="button" variant="ghost" size="sm" onClick={() => props.onSelectTask(selected ? "" : task.id)}>
										<Activity data-icon />
										{selected ? t("workspaceSettings.list.hideDetails") : t("workspaceSettings.list.details")}
									</Button>
								)}
							</div>
							{selected ? <RuntimeTaskEvidence task={task} token={props.token} workspaceID={props.workspaceID} /> : null}
						</div>
					);
				})}
			</div>
			{props.total > props.pageSize ? (
				<TablePagination
					page={clampedPage}
					pageSize={props.pageSize}
					total={props.total}
					onPageChange={props.onPageChange}
				/>
			) : null}
		</div>
	);
}

function TablePagination(props: {
	page: number;
	pageSize: number;
	total: number;
	onPageChange: (page: number) => void;
}) {
	const { t } = useMspaceTranslation();
	const totalPages = Math.max(1, Math.ceil(props.total / props.pageSize));
	const start = props.page * props.pageSize + 1;
	const end = Math.min(props.total, start + props.pageSize - 1);
	const canGoPrevious = props.page > 0;
	const canGoNext = props.page < totalPages - 1;
	return (
		<div className="flex min-w-0 flex-col gap-3 border-t border-[color:var(--line)] bg-[color:var(--block)] px-4 py-3 md:flex-row md:items-center md:justify-between">
			<div className="text-[12px] leading-5 text-[color:var(--muted)]">
				{t("workspaceSettings.list.taskPageRange", { start, end, total: props.total })}
			</div>
			<div className="flex items-center gap-2">
				<Button
					type="button"
					variant="ghost"
					size="sm"
					disabled={!canGoPrevious}
					onClick={() => props.onPageChange(Math.max(0, props.page - 1))}
				>
					<ChevronLeft data-icon />
					{t("workspaceSettings.list.previousPage")}
				</Button>
				<span className="min-w-[72px] text-center text-[12px] leading-5 text-[color:var(--muted)]">
					{t("workspaceSettings.list.pageIndicator", { page: props.page + 1, totalPages })}
				</span>
				<Button
					type="button"
					variant="ghost"
					size="sm"
					disabled={!canGoNext}
					onClick={() => props.onPageChange(Math.min(totalPages - 1, props.page + 1))}
				>
					{t("workspaceSettings.list.nextPage")}
					<ChevronRight data-icon />
				</Button>
			</div>
		</div>
	);
}

function RuntimeTaskIssueLink(props: { task: RuntimeTask; display: RuntimeTaskDisplay }) {
	const { t } = useMspaceTranslation();
	if (!props.task.issueId) {
		return <span className="truncate text-[12px] leading-5 text-[color:var(--faint)]">{t("workspaceSettings.list.noLinkedIssue")}</span>;
	}
	return (
		<Link
			to="/issues/$issueId"
			params={{ issueId: props.task.issueId }}
			search={runtimeTaskIssueSearch(props.task)}
			className="min-w-0 rounded-[6px] px-1 py-1 text-left text-[12px] font-medium leading-5 text-[color:var(--muted-strong)] transition-colors hover:bg-[color:var(--hover)] hover:text-[color:var(--text)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[color:var(--focus)]"
			title={props.display.issue}
		>
			<span className="block truncate">{props.display.issue}</span>
		</Link>
	);
}

function RuntimeTaskEvidence(props: { task: RuntimeTask; token: string; workspaceID: string }) {
	const { t } = useMspaceTranslation();
	const enabled = Boolean(props.token && props.workspaceID && props.task.id);
	const eventsQuery = useQuery({
		queryKey: queryKeys.runtimeTaskEvents(props.workspaceID, props.task.id, props.token),
		queryFn: () => controlPlaneApi.listRuntimeTaskEvents(props.token, props.workspaceID, props.task.id),
		enabled,
		refetchInterval: enabled && ["queued", "claimed", "running"].includes(props.task.status) ? 2_500 : false,
	});
	const logsQuery = useQuery({
		queryKey: queryKeys.runtimeTaskLogs(props.workspaceID, props.task.id, props.token),
		queryFn: () => controlPlaneApi.listRuntimeTaskLogs(props.token, props.workspaceID, props.task.id),
		enabled,
		refetchInterval: enabled && ["claimed", "running"].includes(props.task.status) ? 2_500 : false,
	});
	const events = eventsQuery.data || [];
	const logs = logsQuery.data || [];
	const failed = eventsQuery.error || logsQuery.error;

	return (
		<div className="border-t border-[color:var(--line)] bg-[color:var(--block)] px-4 py-4">
			{failed ? <Notice tone="danger">{failed.message}</Notice> : null}
			<div className="mb-4 grid gap-2 rounded-[8px] bg-[color:var(--surface)] p-3 shadow-[inset_0_0_0_1px_var(--line)] md:grid-cols-4">
				<TaskMeta label={t("workspaceSettings.list.taskKind")} value={props.task.kind} />
				<TaskMeta label={t("workspaceSettings.list.priority")} value={String(props.task.priority)} />
				<TaskMeta label={t("workspaceSettings.list.taskId")} value={shortId(props.task.id)} mono />
				<TaskMeta label={t("workspaceSettings.list.sessionId")} value={props.task.sessionId ? shortId(props.task.sessionId) : t("workspaceSettings.list.none")} mono={Boolean(props.task.sessionId)} />
			</div>
			<div className="grid gap-4 lg:grid-cols-[minmax(0,0.9fr)_minmax(0,1.4fr)]">
				<div className="min-w-0">
					<div className="mb-2 flex items-center gap-2 text-[12px] font-medium leading-5 text-[color:var(--muted)]">
						<Activity data-icon />
						{t("workspaceSettings.list.events")}
					</div>
					{eventsQuery.isPending ? (
						<div className="text-[12px] leading-5 text-[color:var(--muted)]">{t("workspaceSettings.list.loadingEvents")}</div>
					) : events.length === 0 ? (
						<div className="text-[12px] leading-5 text-[color:var(--muted)]">{t("workspaceSettings.list.noTaskEvents")}</div>
					) : (
						<div className="grid gap-2">
							{events.map((event) => (
								<div key={event.id} className="rounded-[7px] bg-[color:var(--surface)] px-3 py-2 shadow-[inset_0_0_0_1px_var(--line)]">
									<div className="flex min-w-0 items-center justify-between gap-2">
										<span className="truncate text-[12px] font-medium leading-5 text-[color:var(--text)]">{runtimeStatusLabel(event.kind)}</span>
										<span className="shrink-0 text-[11px] leading-5 text-[color:var(--faint)]"><RelativeTime value={event.createdAt} /></span>
									</div>
									<div className="mt-1 truncate font-mono text-[11px] leading-5 text-[color:var(--muted)]">{jsonSummary(event.payload)}</div>
								</div>
							))}
						</div>
					)}
				</div>
				<div className="min-w-0">
					<div className="mb-2 flex items-center gap-2 text-[12px] font-medium leading-5 text-[color:var(--muted)]">
						<SquareTerminal data-icon />
						{t("workspaceSettings.list.logs")}
					</div>
					{logsQuery.isPending ? (
						<div className="text-[12px] leading-5 text-[color:var(--muted)]">{t("workspaceSettings.list.loadingLogs")}</div>
					) : logs.length === 0 ? (
						<div className="text-[12px] leading-5 text-[color:var(--muted)]">{t("workspaceSettings.list.noWorkerLogs")}</div>
					) : (
						<div className="max-h-[360px] overflow-auto rounded-[8px] bg-[color:var(--code-bg)] px-3 py-2 font-mono text-[12px] leading-5 text-[color:var(--code-text)]">
							{logs.map((log) => (
								<div key={log.id} className="grid grid-cols-[92px_92px_minmax(0,1fr)] gap-2 border-b border-white/10 py-1 last:border-b-0">
									<span className="truncate text-white/45">{timeLabel(log.createdAt)}</span>
									<span className="truncate text-white/65">{log.stream}</span>
									<span className="min-w-0 whitespace-pre-wrap break-words">{log.message}</span>
								</div>
							))}
						</div>
					)}
				</div>
			</div>
			<div className="mt-4 grid gap-2 rounded-[8px] bg-[color:var(--surface)] p-3 shadow-[inset_0_0_0_1px_var(--line)]">
				<div className="grid gap-2 md:grid-cols-2">
					<RuntimeJSONBlock label={t("workspaceSettings.modal.requiredCapabilities")} value={props.task.requiredCapabilities} />
					<RuntimeJSONBlock label={t("workspaceSettings.modal.payload")} value={props.task.payload} />
				</div>
				{props.task.error ? <RuntimeJSONBlock label={t("workspaceSettings.list.error")} value={{ message: props.task.error }} /> : null}
				{Object.keys(props.task.result || {}).length > 0 ? <RuntimeJSONBlock label={t("workspaceSettings.list.result")} value={props.task.result} /> : null}
			</div>
		</div>
	);
}

function TaskMeta(props: { label: string; value: string; mono?: boolean }) {
	return (
		<div className="min-w-0">
			<div className="text-[11px] font-medium leading-5 text-[color:var(--faint)]">{props.label}</div>
			<div className={cn("truncate text-[12px] leading-5 text-[color:var(--muted-strong)]", props.mono && "font-mono")}>{props.value}</div>
		</div>
	);
}

function RuntimeJSONBlock(props: { label: string; value: Record<string, unknown> }) {
	return (
		<div className="min-w-0">
			<div className="mb-1 text-[11px] font-medium leading-5 text-[color:var(--faint)]">{props.label}</div>
			<pre className="max-h-[160px] overflow-auto rounded-[6px] bg-[color:var(--block)] px-2.5 py-2 text-[11px] leading-5 text-[color:var(--muted-strong)] shadow-[inset_0_0_0_1px_var(--line)]">
				{JSON.stringify(props.value || {}, null, 2)}
			</pre>
		</div>
	);
}

function TableHeader(props: { columns: string; children: ReactNode }) {
	return (
		<div className={cn("grid gap-4 border-b border-[color:var(--line)] px-4 py-2.5 text-[12px] font-medium text-[color:var(--muted)]", props.columns)}>
			{props.children}
		</div>
	);
}

function workerDisplayStatus(worker: RuntimeWorker) {
	if (worker.status.trim().toLowerCase() !== "online") return worker.status;
	const lastSeenAt = new Date(worker.lastSeenAt).getTime();
	if (!Number.isFinite(lastSeenAt)) return "stale";
	return Date.now() - lastSeenAt > ACTIVE_WORKER_MAX_AGE_MS ? "stale" : worker.status;
}

function tokenStatusValue(token: RuntimeRegistrationToken) {
	if (token.revoked) return "revoked";
	if (new Date(token.expiresAt).getTime() < Date.now()) return "expired";
	return "active";
}

function credentialDisplayName(token: RuntimeRegistrationToken) {
	return isDesktopPersonalWorkerCredential(token) ? translate("workspaceSettings.list.desktopAutomaticCredential") : token.name;
}

function credentialMeta(token: RuntimeRegistrationToken) {
	const created = translate("workspaceSettings.list.created");
	if (isDesktopPersonalWorkerCredential(token)) {
		return `${created} ${formatRelativeTime(token.createdAt)} · ${translate("workspaceSettings.list.desktopAutomaticCredentialMeta")}`;
	}
	return `${created} ${formatRelativeTime(token.createdAt)} · ${translate("workspaceSettings.list.manualCredentialMeta")}`;
}

function isDesktopPersonalWorkerCredential(token: RuntimeRegistrationToken) {
	return token.name.trim() === DESKTOP_PERSONAL_WORKER_CREDENTIAL_NAME;
}

type RuntimeTaskDisplay = {
	title: string;
	subtitle: string;
	issue: string;
};

function runtimeTaskDisplay(task: RuntimeTask, issueTitleFallback = ""): RuntimeTaskDisplay {
	const issueTitle = payloadText(task.payload, ["issueTitle"]) || nestedPayloadText(task.payload, ["issue", "title"]) || issueTitleFallback.trim();
	const projectName = payloadText(task.payload, ["projectName"]) || nestedPayloadText(task.payload, ["project", "name"]) || repositoryLabel(task.payload);
	const issueLabel = issueTitle || (task.issueId ? translate("workspaceSettings.list.issueFallback", { id: shortId(task.issueId) }) : "");
	const issueColumn = issueLabel || translate("workspaceSettings.list.noLinkedIssue");
	const projectLabel = projectName || (task.projectId ? translate("workspaceSettings.list.projectFallback", { id: shortId(task.projectId) }) : "");
	const sessionLabel = task.sessionId ? translate("workspaceSettings.list.sessionFallback", { id: shortId(task.sessionId) }) : "";
	const agentEngine = payloadText(task.payload, ["agentEngine", "provider", "agentProfile"]);
	const automation = payloadText(task.payload, ["automation"]);
	const sourceSessionId = payloadText(task.payload, ["sourceSessionId"]);

	if (task.kind === "issue_type_triage") {
		return {
			title: translate("workspaceSettings.list.taskIssueTriage"),
			subtitle: compactTaskParts([projectLabel, taskIdentity(task)]),
			issue: issueColumn,
		};
	}

	if (task.kind === "agent_session") {
		const titleKey = automation === "auto_test_deploy" ? "workspaceSettings.list.taskAutoDeploy" : "workspaceSettings.list.taskAgentSession";
		const actor = agentEngineMention(agentEngine) || translate("workspaceSettings.list.agentFallback");
		return {
			title: translate(titleKey, { actor }),
			subtitle: compactTaskParts([
				projectLabel,
				sessionLabel,
				sourceSessionId ? translate("workspaceSettings.list.sourceSessionFallback", { id: shortId(sourceSessionId) }) : "",
				taskIdentity(task),
			]),
			issue: issueColumn,
		};
	}

	if (task.kind === "protocol_smoke") {
		return {
			title: translate("workspaceSettings.list.taskProtocolSmoke"),
			subtitle: compactTaskParts([projectLabel || issueLabel, taskIdentity(task)]),
			issue: issueColumn,
		};
	}

	if (task.kind === "noop") {
		return {
			title: translate("workspaceSettings.list.taskNoop"),
			subtitle: compactTaskParts([projectLabel || issueLabel, taskIdentity(task)]),
			issue: issueColumn,
		};
	}

	return {
		title: runtimeStatusLabel(task.kind),
		subtitle: compactTaskParts([issueLabel, projectLabel, sessionLabel, taskIdentity(task)]),
		issue: issueColumn,
	};
}

function runtimeTaskIssueSearch(task: RuntimeTask) {
	if (task.sessionId) return { sessionId: task.sessionId };
	if (task.id) return { runtimeTaskId: task.id };
	return {};
}

function taskIdentity(task: RuntimeTask) {
	return translate("workspaceSettings.list.taskIdFallback", { id: shortId(task.id) });
}

function compactTaskParts(parts: string[]) {
	return parts.map((part) => part.trim()).filter(Boolean).join(" · ") || translate("workspaceSettings.list.none");
}

function payloadText(payload: Record<string, unknown>, keys: string[]) {
	for (const key of keys) {
		const value = payload?.[key];
		if (typeof value === "string" && value.trim()) return value.trim();
	}
	return "";
}

function nestedPayloadText(payload: Record<string, unknown>, path: string[]) {
	let value: unknown = payload;
	for (const key of path) {
		if (!value || typeof value !== "object" || Array.isArray(value)) return "";
		value = (value as Record<string, unknown>)[key];
	}
	return typeof value === "string" && value.trim() ? value.trim() : "";
}

function repositoryLabel(payload: Record<string, unknown>) {
	const owner = nestedPayloadText(payload, ["repository", "owner"]);
	const repo = nestedPayloadText(payload, ["repository", "repo"]);
	if (owner && repo) return `${owner}/${repo}`;
	if (repo) return repo;
	const url = nestedPayloadText(payload, ["repository", "url"]);
	return url ? lastPathSegment(url) : "";
}

function lastPathSegment(value: string) {
	const trimmed = value.trim().replace(/\/+$/, "");
	const last = trimmed.split(/[/:]/).filter(Boolean).at(-1) || "";
	return last.endsWith(".git") ? last.slice(0, -4) : last;
}

function shortId(value: string) {
	return value ? value.slice(0, 8) : "";
}

function MemberAvatar(props: { member: WorkspaceMember }) {
	const [failed, setFailed] = useState(false);
	const initial = (props.member.name || props.member.email || "M").trim().slice(0, 1).toUpperCase() || "M";
	useEffect(() => {
		setFailed(false);
	}, [props.member.avatarUrl]);

	return (
		<div className="grid size-8 shrink-0 place-items-center overflow-hidden rounded-full bg-[color:var(--selection)] text-[12px] font-semibold text-[color:var(--muted)]">
			{props.member.avatarUrl && !failed ? (
				<img src={props.member.avatarUrl} alt="" className="size-full object-cover" onError={() => setFailed(true)} />
			) : (
				<span>{initial}</span>
			)}
		</div>
	);
}

function RolePill(props: { role: string }) {
	const role = props.role.trim().toLowerCase();
	const tone = role === "owner"
		? "bg-[color:var(--success-soft)] text-[color:var(--success)]"
		: role === "admin"
			? "bg-[color:var(--blue-soft)] text-[color:var(--accent-blue)]"
			: "bg-[color:var(--block)] text-[color:var(--muted-strong)]";
	return (
		<span className={cn("inline-flex h-6 w-fit items-center rounded-full px-2 text-[12px] font-medium leading-5", tone)}>
			{runtimeStatusLabel(role || "member")}
		</span>
	);
}

function RuntimeStatusPill(props: { status: string }) {
	const normalized = props.status.trim().toLowerCase();
	const tone =
		normalized === "online" || normalized === "active" || normalized === "completed" || normalized === "accepted"
			? "bg-[color:var(--success-soft)] text-[color:var(--success)]"
			: normalized === "running" || normalized === "claimed"
				? "bg-[color:var(--blue-soft)] text-[color:var(--accent-blue)]"
				: normalized === "queued"
					? "bg-[color:var(--block)] text-[color:var(--muted-strong)]"
					: normalized === "failed" || normalized === "expired" || normalized === "revoked"
						? "bg-[color:var(--danger-soft)] text-[color:var(--danger)]"
						: normalized === "draining"
							? "bg-[color:var(--warning-soft)] text-[color:var(--warning)]"
							: "bg-[color:var(--block)] text-[color:var(--muted)]";
	return (
		<span className={cn("inline-flex h-6 w-fit max-w-full justify-self-start items-center gap-1.5 rounded-full px-2 text-[12px] font-medium leading-5", tone)}>
			<Circle data-icon className="size-3" />
			<span className="truncate">{runtimeStatusLabel(normalized)}</span>
		</span>
	);
}

function GitHubAppStatus(props: { installation?: WorkspaceGitHubAppInstallation; loading: boolean }) {
	const { t } = useMspaceTranslation();
	if (props.loading && !props.installation) {
		return <StatusPill>{t("workspaceSettings.section.githubAppChecking")}</StatusPill>;
	}
	const installation = props.installation;
	const status = installation?.status || "unavailable";
	const detail =
		status === "connected"
			? installation?.accountLogin
				? t("workspaceSettings.section.githubAppConnectedAccount", {
					account: installation.accountLogin,
					selection: installation.repositorySelection || t("workspaceSettings.section.githubAppRepositorySelectionAll"),
				})
				: t("workspaceSettings.section.githubAppConnected")
			: status === "needs_attention"
				? installation?.missingPermissions?.length
					? t("workspaceSettings.section.githubAppMissingPermissions", { permissions: installation.missingPermissions.join(", ") })
					: installation?.error || t("workspaceSettings.section.githubAppNeedsAttention")
				: status === "not_connected"
					? t("workspaceSettings.section.githubAppNotConnectedDescription")
					: installation?.error || t("workspaceSettings.section.githubAppUnavailableDescription");
	return (
		<div className="grid max-w-[360px] justify-items-end gap-1 text-right">
			<GitHubAppStatusPill status={status} />
			<div className="text-[12px] leading-5 text-[color:var(--muted)]">{detail}</div>
		</div>
	);
}

function GitHubAppStatusPill(props: { status: string }) {
	const normalized = props.status.trim().toLowerCase();
	const tone =
		normalized === "connected"
			? "bg-[color:var(--success-soft)] text-[color:var(--success)]"
			: normalized === "needs_attention"
				? "bg-[color:var(--warning-soft)] text-[color:var(--warning)]"
				: normalized === "not_connected"
					? "bg-[color:var(--block)] text-[color:var(--muted-strong)]"
					: "bg-[color:var(--danger-soft)] text-[color:var(--danger)]";
	return (
		<span className={cn("inline-flex h-6 w-fit max-w-full items-center gap-1.5 rounded-full px-2 text-[12px] font-medium leading-5", tone)}>
			<Circle data-icon className="size-3" />
			<span className="truncate">{githubAppStatusLabel(normalized)}</span>
		</span>
	);
}

function githubAppStatusLabel(status: string) {
	switch (status) {
		case "connected":
			return translate("workspaceSettings.section.githubAppConnected");
		case "needs_attention":
			return translate("workspaceSettings.section.githubAppNeedsAttention");
		case "not_connected":
			return translate("workspaceSettings.section.githubAppNotConnected");
		default:
			return translate("workspaceSettings.section.githubAppUnavailable");
	}
}

function StatusPill(props: { children: ReactNode }) {
	return (
		<span className="inline-flex h-7 items-center rounded-full bg-[color:var(--block)] px-2.5 text-[12px] font-medium leading-5 text-[color:var(--muted-strong)] shadow-[inset_0_0_0_1px_var(--line)]">
			{props.children}
		</span>
	);
}

function EmptyRuntimeBlock(props: { icon: LucideIcon; title: string; body: string }) {
	const Icon = props.icon;
	return (
		<div className="grid place-items-center px-6 py-10 text-center">
			<div className="grid size-9 place-items-center rounded-[9px] bg-[color:var(--block)] text-[color:var(--muted)] shadow-[inset_0_0_0_1px_var(--line)]">
				<Icon data-icon />
			</div>
			<div className="mt-3 text-[14px] font-semibold leading-5 text-[color:var(--text)]">{props.title}</div>
			<div className="mt-1 max-w-[46ch] text-[13px] leading-6 text-[color:var(--muted)] text-pretty">{props.body}</div>
		</div>
	);
}

function LoadingBlock(props: { children: ReactNode }) {
	return <div className="px-4 py-6 text-[13px] leading-6 text-[color:var(--muted)]">{props.children}</div>;
}

function invitationStatus(invitation: WorkspaceInvitation) {
	if (invitation.revoked) return "revoked";
	if (invitation.acceptedAt) return "accepted";
	if (new Date(invitation.expiresAt).getTime() < Date.now()) return "expired";
	return "pending";
}

function activeInvitationCount(invitations: WorkspaceInvitation[]) {
	return invitations.filter((invitation) => invitationStatus(invitation) === "pending").length;
}

function buildInviteLink(token: string) {
	const server = typeof window === "undefined" ? "" : getControlPlaneBaseUrl();
	const serverQuery = server ? `?server=${encodeURIComponent(server)}` : "";
	return `mspace://invite/${encodeURIComponent(token)}${serverQuery}`;
}

function Modal(props: { title: string; description: string; onClose: () => void; children: ReactNode }) {
	const { t } = useMspaceTranslation();

	return (
		<div className="fixed inset-0 z-50 grid place-items-center bg-black/20 px-4 py-6" role="dialog" aria-modal="true" aria-labelledby="workspace-settings-modal-title">
			<div className="w-full max-w-[640px] min-w-0 overflow-hidden rounded-[12px] bg-[color:var(--paper)] shadow-[0_24px_80px_rgba(0,0,0,0.18),inset_0_0_0_1px_var(--line)]">
				<div className="flex items-start justify-between gap-4 border-b border-[color:var(--line)] px-5 py-4">
					<div className="min-w-0">
						<h2 id="workspace-settings-modal-title" className="text-[17px] font-semibold leading-6 text-[color:var(--text)]">{props.title}</h2>
						<p className="mt-1 text-[13px] leading-5 text-[color:var(--muted)] text-pretty">{props.description}</p>
					</div>
					<Button type="button" variant="ghost" size="icon" aria-label={t("common.close")} onClick={props.onClose}>
						<X data-icon />
					</Button>
				</div>
				<div className="px-5 py-5">{props.children}</div>
			</div>
		</div>
	);
}

function jsonSummary(value: Record<string, unknown>) {
	const keys = Object.keys(value || {});
	if (keys.length === 0) return translate("workspaceSettings.list.none");
	const summary = keys.slice(0, 4).map((key) => `${key}:${String(value[key])}`).join(", ");
	return keys.length > 4 ? `${summary}, ...` : summary;
}

function timeLabel(value: string) {
	if (!value) return "";
	const date = new Date(value);
	if (Number.isNaN(date.getTime())) return "";
	return date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" });
}

function runtimeStatusLabel(value: string) {
	const label = translate(`workspaceSettings.status.${value}`, { defaultValue: "" });
	if (label) return label;
	return value.replace(/[_-]+/g, " ").replace(/\b\w/g, (letter) => letter.toUpperCase());
}
