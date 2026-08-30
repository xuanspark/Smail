import { createRouter, createWebHashHistory } from "vue-router";
import {useGlobalStatusStore} from "@/stores/useGlobalStatusStore";

const router = createRouter({
    history: createWebHashHistory(),
    routes: [
        {
            path: "/",
            name: "home",
            component: () => import("../views/List/List.vue"),
            meta: {requiresAuth: true}
        },
        {
            path: "/list",
            name: "list",
            component: () => import("../views/List/List.vue"),
            meta: {requiresAuth: true}
        },
        {
            path: "/editer",
            name: "editer",
            component: () => import("../views/Editer/Editer.vue"),
            meta: {requiresAuth: true}
        },
        {
            path: "/login",
            name: "login",
            component: () => import("../views/Login/Login.vue")
        },
        {
            path: "/login-verification",
            name: "login-verification",
            component: () => import("../views/LoginVerification/LoginVerification.vue")
        },
        {
            path: "/force-password-change",
            name: "force-password-change",
            component: () => import("../views/ForcePasswordChange/ForcePasswordChange.vue"),
            meta: {requiresAuth: true}
        },
        {
            path: "/setup",
            name: "setup",
            component: () => import("../views/Setup/Setup.vue")
        },
        {
            path: "/forgot-password",
            name: "forgot-password",
            component: () => import("../views/PasswordRecovery/PasswordRecovery.vue")
        },
        {
            path: "/reset-blocked",
            name: "reset-blocked",
            component: () => import("../views/ResetBlocked/ResetBlocked.vue")
        },
        {
            path: "/detail/:id",
            name: "detail",
            component: () => import("../views/EmailDetail/EmailDetail.vue"),
            meta: {requiresAuth: true}
        }
    ]
});

router.beforeEach(async (to) => {
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
