import { defineStore } from "pinia";
import { http } from "@/utils/axios";
import {
    AUTH_STATUS,
    classifyAuthenticationError,
    classifyAuthenticationResponse,
    runSharedAuthRefresh
} from "@/utils/auth.mjs";
import {getSafeLoginRedirect} from "@/utils/navigation.mjs";

const useGlobalStatusStore = defineStore("useGlobalStatusStore", {
    state() {
        return {
            userInfos: {},
            mobileDrawerVisible: false,
            settingsDrawerVisible: false,
            authChecked: false,
            authLoading: false,
            authStatus: AUTH_STATUS.UNKNOWN,
            authError: "",
            authRetryPath: "/",
            startupError: "",
            pendingLogin: null,
            recoveryPromptDismissed: false
        };
    },
    getters: {
        isLogin(state) {
            return Object.keys(state.userInfos).length !== 0;
        },
        role(state) {
            return state.userInfos.role || "";
        },
        capabilities(state) {
            return Array.isArray(state.userInfos.capabilities)
                ? state.userInfos.capabilities
                : [];
        },
        startupBlocked(state) {
            return Boolean(state.startupError)
                || (state.authLoading && Object.keys(state.userInfos).length === 0)
                || state.authStatus === AUTH_STATUS.UNAVAILABLE;
        }
    },
    actions: {
        refresh() {
            return runSharedAuthRefresh(async () => {
                this.authLoading = true;
                this.authStatus = AUTH_STATUS.LOADING;
                this.authError = "";
                try {
                    const response = await http.post("/api/v1/user/info", {});
                    const result = classifyAuthenticationResponse(response);
                    if (result === AUTH_STATUS.AUTHENTICATED) {
                        this.setUser(response.data, false);
                    } else if (result === AUTH_STATUS.UNAUTHENTICATED) {
                        this.clear();
                    } else if (result === AUTH_STATUS.NEEDS_SETUP) {
                        this.markSetupRequired();
                    } else {
                        this.markAuthUnavailable();
                    }
                    return result;
                } catch (error) {
                    const result = classifyAuthenticationError(error, {authenticationRequest: true});
                    if (result === AUTH_STATUS.UNAUTHENTICATED) {
                        this.clear();
                    } else if (result === AUTH_STATUS.NEEDS_SETUP) {
                        this.markSetupRequired();
                    } else {
                        this.markAuthUnavailable();
                    }
                    return result;
                } finally {
                    this.authLoading = false;
                }
            });
        },
        init(callback) {
            return this.refresh().then((status) => {
                if (typeof callback === "function") {
                    callback(status);
                }
                return status;
            });
        },
        clear() {
            this.userInfos = {};
            this.pendingLogin = null;
            this.recoveryPromptDismissed = false;
            this.authChecked = true;
            this.authStatus = AUTH_STATUS.UNAUTHENTICATED;
            this.authError = "";
        },
        setUser(payload, resetPrompt = false) {
            const previousAccount = this.userInfos?.account || "";
            this.userInfos = payload && typeof payload === "object" ? { ...payload } : {};
            if (resetPrompt || previousAccount !== this.userInfos.account) {
                this.recoveryPromptDismissed = false;
            }
            this.authChecked = true;
            this.authStatus = AUTH_STATUS.AUTHENTICATED;
            this.authError = "";
        },
        markAuthUnavailable() {
            this.authChecked = true;
            this.authStatus = AUTH_STATUS.UNAVAILABLE;
            this.authError = "认证服务暂时不可用，请检查网络后重试";
        },
        markSetupRequired() {
            this.userInfos = {};
            this.pendingLogin = null;
            this.recoveryPromptDismissed = false;
            this.authChecked = true;
            this.authStatus = AUTH_STATUS.NEEDS_SETUP;
            this.authError = "";
        },
        resetAuthAvailability() {
            this.authChecked = false;
            this.authStatus = AUTH_STATUS.UNKNOWN;
            this.authError = "";
        },
        setAuthRetryPath(path) {
            this.authRetryPath = typeof path === "string" && path ? path : "/";
        },
        clearAuthRetryPath() {
            this.authRetryPath = "/";
        },
        setStartupError(message) {
            this.startupError = message || "页面加载失败，请刷新后重试";
        },
        clearStartupError() {
            this.startupError = "";
        },
        dismissRecoveryPrompt() {
            this.recoveryPromptDismissed = true;
        },
        setPendingLogin(payload) {
            this.pendingLogin = {
                challengeId: payload?.challenge_id || "",
                account: payload?.account || "",
                email: payload?.email || "",
                expiresAt: payload?.expires_at || "",
                redirect: getSafeLoginRedirect(payload?.redirect)
            };
        },
        clearPendingLogin() {
            this.pendingLogin = null;
        },
        hasCapability(capability) {
            if (!capability) {
                return true;
            }
            return this.role === "super_admin" || this.capabilities.includes(capability);
        }
    }
});

export { useGlobalStatusStore };
