import { useEffect, useState, type FormEvent, type ReactNode } from "react";
import { useMutation } from "@tanstack/react-query";
import { CheckCircle2, IdCard, LinkIcon, Save, UserRound, type LucideIcon } from "lucide-react";
import { controlPlaneApi, setStoredAuthIdentity } from "@mspace/core";
import { useMspaceTranslation, t as translate } from "@mspace/i18n";
import { Button, Input, Notice, PageFrame } from "@mspace/ui";
import { useMspaceAuth } from "./auth-context";

export function AccountSettingsPage() {
	const { t } = useMspaceTranslation();
	const auth = useMspaceAuth();
	const user = auth.user;
	const isSignedIn = auth.status === "signed-in" && Boolean(auth.token && user);
	const [name, setName] = useState(user?.name || "");
	const [avatarUrl, setAvatarUrl] = useState(user?.avatarUrl || "");
	const trimmedName = name.trim();
	const trimmedAvatarUrl = avatarUrl.trim();
	const profileDirty = Boolean(
		user &&
			trimmedName !== "" &&
			(trimmedName !== user.name || trimmedAvatarUrl !== (user.avatarUrl || "")),
	);
	const identityLabel = accountIdentityLabel(auth.identityProvider, auth.identityLogin);

	useEffect(() => {
		setName(user?.name || "");
		setAvatarUrl(user?.avatarUrl || "");
	}, [user?.id, user?.name, user?.avatarUrl]);

	const updateProfile = useMutation({
		mutationFn: () => {
			if (!auth.token) throw new Error(t("workspace.signInRequired"));
			return controlPlaneApi.updateCurrentUserProfile(auth.token, {
				name: trimmedName,
				avatarUrl: trimmedAvatarUrl,
			});
		},
		onSuccess: async (result) => {
			setStoredAuthIdentity(result.user);
			setName(result.user.name);
			setAvatarUrl(result.user.avatarUrl || "");
			await auth.refreshAuth?.();
		},
	});

	function submitProfile(event: FormEvent<HTMLFormElement>) {
		event.preventDefault();
		if (!isSignedIn || !profileDirty || updateProfile.isPending) return;
		updateProfile.mutate();
	}

	return (
		<PageFrame title={t("accountSettings.title")} subtitle={t("accountSettings.subtitle")}>
			<div className="grid gap-6">
				{!isSignedIn ? <Notice>{t("workspace.signInRequired")}</Notice> : null}
				{updateProfile.error ? <Notice tone="danger">{updateProfile.error.message || t("accountSettings.saveFailed")}</Notice> : null}
				<section className="grid gap-2">
					<div className="flex min-w-0 items-end justify-between gap-4 px-0.5">
						<div className="min-w-0">
							<h2 className="text-[14px] font-semibold leading-5 text-[color:var(--text)]">{t("accountSettings.profileTitle")}</h2>
							<p className="mt-1 max-w-[72ch] text-[12px] leading-5 text-[color:var(--muted)] text-pretty">{t("accountSettings.profileDescription")}</p>
						</div>
						{updateProfile.isSuccess && !profileDirty ? (
							<span className="inline-flex h-8 items-center gap-1.5 rounded-[7px] px-2 text-[12px] font-medium leading-5 text-[color:var(--muted)]">
								<CheckCircle2 data-icon className="size-4 text-[color:var(--success)]" />
								{t("accountSettings.saved")}
							</span>
						) : null}
					</div>
					<form className="overflow-hidden rounded-[10px] bg-[color:var(--surface)] shadow-[inset_0_0_0_1px_var(--line)]" onSubmit={submitProfile}>
						<div className="grid min-w-0 gap-4 border-b border-[color:var(--line)] px-4 py-4 md:grid-cols-[44px_minmax(0,1fr)]">
							<AccountAvatar name={name} avatarUrl={avatarUrl} />
							<div className="min-w-0">
								<div className="text-[13px] font-medium leading-5 text-[color:var(--text)]">{trimmedName || t("workspace.mspaceUser")}</div>
								<div className="mt-1 truncate text-[12px] leading-5 text-[color:var(--muted)]">{identityLabel}</div>
							</div>
						</div>
						<ProfileRow
							icon={UserRound}
							title={t("accountSettings.displayName")}
							description={t("accountSettings.displayNameDescription")}
							control={
								<div className="w-full min-w-0 md:w-[360px]">
									<Input
										value={name}
										disabled={!isSignedIn || updateProfile.isPending}
										maxLength={120}
										aria-label={t("accountSettings.displayName")}
										onChange={(event) => setName(event.target.value)}
									/>
								</div>
							}
						/>
						<ProfileRow
							icon={LinkIcon}
							title={t("accountSettings.avatarUrl")}
							description={t("accountSettings.avatarUrlDescription")}
							control={
								<div className="w-full min-w-0 md:w-[360px]">
									<Input
										value={avatarUrl}
										disabled={!isSignedIn || updateProfile.isPending}
										maxLength={2048}
										placeholder="https://..."
										aria-label={t("accountSettings.avatarUrl")}
										onChange={(event) => setAvatarUrl(event.target.value)}
									/>
									<div className="mt-1 text-[11px] leading-4 text-[color:var(--faint)]">{t("accountSettings.avatarUrlHint")}</div>
								</div>
							}
						/>
						<ProfileRow
							icon={IdCard}
							title={t("accountSettings.identity")}
							description={t("accountSettings.identityDescription")}
							control={
								<div className="w-full min-w-0 rounded-[8px] bg-[color:var(--block)] px-3 py-2 text-[12px] leading-5 text-[color:var(--muted)] md:w-[360px]">
									<div className="truncate font-medium text-[color:var(--muted-strong)]">{identityLabel}</div>
									<div className="mt-0.5 truncate">{t("accountSettings.identityReadOnly")}</div>
								</div>
							}
						/>
						<div className="flex justify-end border-t border-[color:var(--line)] px-4 py-3">
							<Button type="submit" size="sm" disabled={!isSignedIn || !profileDirty || updateProfile.isPending || trimmedName === ""}>
								<Save data-icon />
								{updateProfile.isPending ? t("accountSettings.saving") : t("accountSettings.save")}
							</Button>
						</div>
					</form>
				</section>
			</div>
		</PageFrame>
	);
}

function ProfileRow(props: {
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

function AccountAvatar(props: { name?: string; avatarUrl?: string }) {
	const [failed, setFailed] = useState(false);
	const initial = props.name?.trim().slice(0, 1).toUpperCase() || "M";

	useEffect(() => {
		setFailed(false);
	}, [props.avatarUrl]);

	return (
		<div className="grid size-11 shrink-0 place-items-center overflow-hidden rounded-full bg-[color:var(--selection)] text-[15px] font-semibold text-[color:var(--muted)]">
			{props.avatarUrl && !failed ? (
				<img src={props.avatarUrl} alt="" className="size-full object-cover" onError={() => setFailed(true)} />
			) : (
				<span>{initial}</span>
			)}
		</div>
	);
}

function accountIdentityLabel(provider?: string, login?: string) {
	const normalizedProvider = provider?.trim().toLowerCase();
	if (normalizedProvider === "github") {
		return login ? translate("workspace.githubIdentityConnectedAs", { login }) : translate("workspace.githubConnected");
	}
	if (normalizedProvider === "password") {
		return login ? translate("workspace.localAccountSignedInAs", { login }) : translate("workspace.localAccount");
	}
	return login || translate("accountSettings.identityUnknown");
}
