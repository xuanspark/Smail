import { createRouter, createWebHashHistory } from "vue-router";
import ListView from "../views/List/List.vue";
import EditerView from "../views/Editer/Editer.vue";
import LoginView from "../views/Login/Login.vue";
import EmailDetailView from "../views/EmailDetail/EmailDetail.vue";
import SetupView from "../views/Setup/Setup.vue";
import PasswordRecoveryView from "../views/PasswordRecovery/PasswordRecovery.vue";
import ResetBlockedView from "../views/ResetBlocked/ResetBlocked.vue";
import LoginVerificationView from "../views/LoginVerification/LoginVerification.vue";
import ForcePasswordChangeView from "../views/ForcePasswordChange/ForcePasswordChange.vue";

const router = createRouter({
    history: createWebHashHistory(),
    routes: [
        {
            path: "/",
            name: "home",
            component: ListView,
            meta: {requiresAuth: true}
        },
        {
            path: "/list",
            name: "list",
            component: ListView,
            meta: {requiresAuth: true}
        },
        {
            path: "/editer",
            name: "editer",
            component: EditerView,
            meta: {requiresAuth: true}
        },
        {
            path: "/login",
            name: "login",
            component: LoginView
        },
        {
            path: "/login-verification",
            name: "login-verification",
            component: LoginVerificationView
        },
        {
            path: "/force-password-change",
            name: "force-password-change",
            component: ForcePasswordChangeView,
            meta: {requiresAuth: true}
        },
        {
            path: "/setup",
            name: "setup",
            component: SetupView
        },
        {
            path: "/forgot-password",
            name: "forgot-password",
            component: PasswordRecoveryView
        },
        {
            path: "/reset-blocked",
            name: "reset-blocked",
            component: ResetBlockedView
        },
        {
            path: "/detail/:id",
            name: "detail",
            component: EmailDetailView,
            meta: {requiresAuth: true}
        }
    ]
});

router.beforeEach(async (to) => {
    const {useGlobalStatusStore} = await import("@/stores/useGlobalStatusStore");
    const globalStatus = useGlobalStatusStore();
    const needsAuthentication = to.meta.requiresAuth || to.name === "force-password-change";
    const authenticated = globalStatus.isLogin || (needsAuthentication && await globalStatus.refresh());

    if (!authenticated && needsAuthentication) {
        return {
            name: "login",
            query: {redirect: to.fullPath}
        };
    }

    if (to.name === "force-password-change") {
        if (!globalStatus.userInfos.must_change_password) {
            return {name: "list"};
        }
        return true;
    }

    if (authenticated && globalStatus.userInfos.must_change_password) {
        return {name: "force-password-change"};
    }

    if (!to.meta.requiresAuth) {
        return true;
    }

    if (to.meta.capability && !globalStatus.hasCapability(to.meta.capability)) {
        return {name: "list"};
    }

    return true;
});

export {router};
