import { defineStore } from "pinia";
import { http } from "@/utils/axios";

let authRefreshPromise = null;

const useGlobalStatusStore = defineStore("useGlobalStatusStore", {
    state() {
        return {
            userInfos: {},
            mobileDrawerVisible: false,
            settingsDrawerVisible: false,
            authChecked: false,
            authLoading: false,
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
        }
    },
    actions: {
        refresh() {
            if (authRefreshPromise) {
                return authRefreshPromise;
            }

            this.authLoading = true;
            authRefreshPromise = (async () => {
                try {
                    const response = await http.post("/api/v1/user/info", {});
                    if (response.errorNo === 0 && response.data) {
                        this.setUser(response.data, false);
                        this.authChecked = true;
                        return true;
                    }
                    this.clear();
                    return false;
                } catch (error) {
                    this.clear();
                    return false;
                } finally {
                    this.authLoading = false;
                    authRefreshPromise = null;
                }
            })();

            return authRefreshPromise;
        },
        init(callback) {
            return this.refresh().then((authenticated) => {
                if (typeof callback === "function") {
                    callback(authenticated);
                }
                return authenticated;
            });
        },
        clear() {
            this.userInfos = {};
            this.pendingLogin = null;
            this.recoveryPromptDismissed = false;
            this.authChecked = true;
        },
        setUser(payload, resetPrompt = false) {
            const previousAccount = this.userInfos?.account || "";
            this.userInfos = payload && typeof payload === "object" ? { ...payload } : {};
            if (resetPrompt || previousAccount !== this.userInfos.account) {
                this.recoveryPromptDismissed = false;
            }
            this.authChecked = true;
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
                redirect: payload?.redirect || "/"
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
