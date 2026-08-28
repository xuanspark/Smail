import { defineStore } from "pinia";
import { http } from "@/utils/axios";

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
        async refresh() {
            if (this.authLoading) {
                return this.isLogin;
            }

            this.authLoading = true;
            try {
                const response = await http.post("/api/user/info", {});
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
            }
        },
        init(callback) {
            this.refresh().then(() => {
                if (typeof callback === "function") {
                    callback();
                }
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
