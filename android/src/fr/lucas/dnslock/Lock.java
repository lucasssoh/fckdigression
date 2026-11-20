package fr.lucas.dnslock;

import android.app.admin.DevicePolicyManager;
import android.content.ComponentName;
import android.content.Context;
import android.content.SharedPreferences;
import android.os.UserManager;
import android.provider.Settings;

import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.util.ArrayList;
import java.util.HashSet;
import java.util.List;
import java.util.Set;

/**
 * All the lock logic, shared by the computer tool (adb broadcasts) and the on-phone screen.
 *
 * Note: the package id stays fr.lucas.dnslock on purpose. The app is device owner on
 * phones in the field, and a new id (or signing key) would mean a factory reset.
 */
final class Lock {

    static final int APP_VERSION = 2;

    private static final String PREFS = "lock";
    static final int FREE_ATTEMPTS = 5;
    private static final long BASE_LOCKOUT_MS = 30L * 60 * 1000;      // 30 min
    private static final long MAX_LOCKOUT_MS = 24L * 60 * 60 * 1000;  // 24 h
    static final int MAX_CODE_LEN = 64;

    // checkPin results
    static final int PIN_OK = 0;
    static final int PIN_WRONG = 1;
    static final int PIN_WRONG_LENGTH = 2;
    static final int PIN_WAIT = 3;
    static final int PIN_NO_CODE = 4;

    /** Restrictions applied by the DNS guard (debugging is added separately, see finalizeLock). */
    private static final String[][] NAMES = {
            {UserManager.DISALLOW_CONFIG_PRIVATE_DNS, "dns"},
            {UserManager.DISALLOW_ADD_USER, "users"},
            {UserManager.DISALLOW_FACTORY_RESET, "reset"},
            {UserManager.DISALLOW_CONFIG_VPN, "vpn"},
            {UserManager.DISALLOW_DEBUGGING_FEATURES, "debug"},
    };

    private Lock() {}

    static ComponentName admin(Context c) {
        return new ComponentName(c, AdminReceiver.class);
    }

    static DevicePolicyManager dpm(Context c) {
        return (DevicePolicyManager) c.getSystemService(Context.DEVICE_POLICY_SERVICE);
    }

    static UserManager um(Context c) {
        return (UserManager) c.getSystemService(Context.USER_SERVICE);
    }

    static SharedPreferences prefs(Context c) {
        return c.getSharedPreferences(PREFS, Context.MODE_PRIVATE);
    }

    static boolean isOwner(Context c) {
        return dpm(c).isDeviceOwnerApp(c.getPackageName());
    }

    static boolean has(Context c, String restriction) {
        return um(c).hasUserRestriction(restriction);
    }

    static boolean isLocked(Context c) {
        return isOwner(c) && has(c, UserManager.DISALLOW_CONFIG_PRIVATE_DNS);
    }

    static String dnsHost(Context c) {
        if (!isOwner(c)) return null;
        String h = dpm(c).getGlobalPrivateDnsHost(admin(c));
        return h != null ? h : prefs(c).getString("dns", null);
    }

    private static String shortName(String restriction) {
        for (String[] n : NAMES) if (n[0].equals(restriction)) return n[1];
        return restriction;
    }

    /** Points Private DNS at the host. Returns null on success, or an error message. */
    private static String setDns(Context c, String dns) {
        DevicePolicyManager dpm = dpm(c);
        ComponentName a = admin(c);
        // The DNS restriction must be lifted to change the server.
        dpm.clearUserRestriction(a, UserManager.DISALLOW_CONFIG_PRIVATE_DNS);

        int r = dpm.setGlobalPrivateDnsModeSpecifiedHost(a, dns);
        if (r == DevicePolicyManager.PRIVATE_DNS_SET_NO_ERROR) return null;

        // Fallback: on some phones/networks the DPM DoT check fails while the system setting works.
        try {
            dpm.setGlobalSetting(a, "private_dns_mode", "hostname");
            dpm.setGlobalSetting(a, "private_dns_specifier", dns);
            try { Thread.sleep(5000); } catch (InterruptedException ignored) {}
            String mode = Settings.Global.getString(c.getContentResolver(), "private_dns_mode");
            String spec = Settings.Global.getString(c.getContentResolver(), "private_dns_specifier");
            if ("hostname".equals(mode) && dns.equals(spec)) return null;
        } catch (Exception ignored) {
        }
        return r == DevicePolicyManager.PRIVATE_DNS_SET_ERROR_HOST_NOT_SERVING
                ? dns + " doesn't answer DNS-over-TLS (wrong hostname, or no internet?)"
                : "could not set Private DNS (code " + r + ")";
    }

    /**
     * Sets DNS and restrictions (debugging is only blocked by finalizeLock, so adb survives until the end).
     * keepCode: keep the parent code already saved on the phone (used to update the app).
     */
    static String applyLock(Context c, String dns, String salt, String hash, int len, boolean keepCode,
                            boolean blockReset, boolean blockVpn, boolean blockDebug) {
        if (!isOwner(c)) return "ERROR: the app is not device owner";
        if (dns == null || dns.isEmpty()) return "ERROR: missing DNS server";
        SharedPreferences p = prefs(c);
        if (keepCode) {
            if (!p.contains("hash")) return "ERROR: no parent code saved on the phone, lock without --keep-code";
        } else if (salt == null || hash == null || hash.length() != 64) {
            return "ERROR: missing parent code";
        }

        String err = setDns(c, dns);
        if (err != null) return "ERROR: " + err;

        List<String> restrictions = new ArrayList<>();
        restrictions.add(UserManager.DISALLOW_CONFIG_PRIVATE_DNS);
        restrictions.add(UserManager.DISALLOW_ADD_USER);        // no second user / "second space"
        if (blockReset) restrictions.add(UserManager.DISALLOW_FACTORY_RESET);
        if (blockVpn) restrictions.add(UserManager.DISALLOW_CONFIG_VPN); // a VPN bypasses Private DNS
        DevicePolicyManager dpm = dpm(c);
        for (String s : restrictions) dpm.addUserRestriction(admin(c), s);

        Set<String> saved = new HashSet<>(restrictions);
        if (blockDebug) saved.add(UserManager.DISALLOW_DEBUGGING_FEATURES);

        SharedPreferences.Editor e = p.edit()
                .putString("dns", dns)
                .putStringSet("restrictions", saved)
                .remove("lastRestrictions")
                .putBoolean("pendingDebugBlock", blockDebug);
        if (!keepCode) {
            e.putString("salt", salt).putString("hash", hash).putInt("fails", 0).putLong("lockedUntil", 0);
            if (len > 0) e.putInt("len", len); else e.remove("len");
        }
        e.apply();

        StringBuilder names = new StringBuilder();
        for (String s : restrictions) names.append(names.length() > 0 ? "," : "").append(shortName(s));
        return "OK: lock applied, dns=" + dns + ", blocked=" + names + (keepCode ? ", code=kept" : "");
    }

    /** Last step: blocks USB debugging (the adb connection drops right after). */
    static String finalizeLock(Context c) {
        if (!isOwner(c)) return "ERROR: the app is not device owner";
        SharedPreferences p = prefs(c);
        if (!p.getBoolean("pendingDebugBlock", false)) return "OK: nothing to finalize";
        dpm(c).addUserRestriction(admin(c), UserManager.DISALLOW_DEBUGGING_FEATURES);
        p.edit().putBoolean("pendingDebugBlock", false).apply();
        return "OK: USB debugging blocked";
    }

    static String status(Context c) {
        StringBuilder sb = new StringBuilder();
        boolean owner = isOwner(c);
        SharedPreferences p = prefs(c);
        sb.append("app_version=").append(APP_VERSION);
        sb.append(" owner=").append(owner);
        if (owner) {
            DevicePolicyManager dpm = dpm(c);
            int mode = dpm.getGlobalPrivateDnsMode(admin(c));
            String modeName = mode == DevicePolicyManager.PRIVATE_DNS_MODE_PROVIDER_HOSTNAME ? "hostname"
                    : mode == DevicePolicyManager.PRIVATE_DNS_MODE_OPPORTUNISTIC ? "opportunistic"
                    : mode == DevicePolicyManager.PRIVATE_DNS_MODE_OFF ? "off" : "unknown";
            sb.append(" dns_mode=").append(modeName);
            String host = dpm.getGlobalPrivateDnsHost(admin(c));
            if (host != null) sb.append(" dns=").append(host);
            for (String[] n : NAMES) sb.append(" lock_").append(n[1]).append('=').append(has(c, n[0]));
        }
        sb.append(" code_set=").append(p.contains("hash"));
        sb.append(" code_len=").append(p.getInt("len", 0));
        sb.append(" fails=").append(p.getInt("fails", 0));
        sb.append(" wait_s=").append(waitMs(c) / 1000);
        return sb.toString();
    }

    /** Milliseconds to wait before the next code attempt (0 = allowed). */
    static long waitMs(Context c) {
        long until = prefs(c).getLong("lockedUntil", 0);
        return Math.max(0, until - System.currentTimeMillis());
    }

    /** Saved code length, or 0 if unknown (locks made by app version 1). */
    static int codeLength(Context c) {
        return prefs(c).getInt("len", 0);
    }

    static boolean hasCode(Context c) {
        return prefs(c).contains("hash");
    }

    /** Wrong codes left before the next wait. */
    static int attemptsLeft(Context c) {
        int fails = prefs(c).getInt("fails", 0);
        return fails < FREE_ATTEMPTS ? FREE_ATTEMPTS - fails : 1;
    }

    /**
     * Checks the parent code. After 5 mistakes each new one starts a growing wait (30 min, 1 h, … 24 h).
     * An entry of the wrong length is never counted, so a missed digit during dictation costs nothing.
     */
    static int checkPin(Context c, String pin) {
        SharedPreferences p = prefs(c);
        String salt = p.getString("salt", null);
        String hash = p.getString("hash", null);
        if (salt == null || hash == null) return PIN_NO_CODE;
        if (pin == null || pin.isEmpty() || !pin.matches("[0-9]+")) return PIN_WRONG_LENGTH;
        if (waitMs(c) > 0) return PIN_WAIT;
        int len = p.getInt("len", 0);
        if (len > 0 && pin.length() != len) return PIN_WRONG_LENGTH;

        if (sha256(salt + ":" + pin).equalsIgnoreCase(hash)) {
            SharedPreferences.Editor e = p.edit().putInt("fails", 0).putLong("lockedUntil", 0);
            if (len == 0) e.putInt("len", pin.length()); // learn the length (locks made by version 1)
            e.apply();
            return PIN_OK;
        }
        int fails = p.getInt("fails", 0) + 1;
        long lockout = 0;
        if (fails >= FREE_ATTEMPTS) {
            int over = Math.min(fails - FREE_ATTEMPTS, 10);
            lockout = Math.min(BASE_LOCKOUT_MS << over, MAX_LOCKOUT_MS);
        }
        p.edit().putInt("fails", fails)
                .putLong("lockedUntil", lockout > 0 ? System.currentTimeMillis() + lockout : 0)
                .apply();
        return PIN_WRONG;
    }

    /** Human message for a failed checkPin, shared by the phone screen and adb replies. */
    static String pinError(Context c, int r) {
        switch (r) {
            case PIN_WAIT:
                return "Too many wrong codes. Try again in " + (waitMs(c) / 60000 + 1) + " min.";
            case PIN_WRONG_LENGTH:
                int len = codeLength(c);
                return len > 0 ? "The code has " + len + " digits. This entry wasn't counted."
                        : "Enter digits only.";
            case PIN_NO_CODE:
                return "No parent code is saved on this phone.";
            default:
                long wait = waitMs(c);
                if (wait > 0) return "Wrong code. Next try in " + (wait / 60000 + 1) + " min.";
                int left = attemptsLeft(c);
                return "Wrong code. " + left + (left == 1 ? " attempt" : " attempts") + " left before a wait.";
        }
    }

    /** Lifts every restriction set by the app. DNS stays set but can be changed again. */
    static void unlock(Context c) {
        if (!isOwner(c)) return;
        DevicePolicyManager dpm = dpm(c);
        ComponentName a = admin(c);
        SharedPreferences p = prefs(c);
        Set<String> saved = new HashSet<>(p.getStringSet("restrictions", new HashSet<String>()));
        for (String s : saved) dpm.clearUserRestriction(a, s);
        dpm.clearUserRestriction(a, UserManager.DISALLOW_CONFIG_PRIVATE_DNS);
        dpm.clearUserRestriction(a, UserManager.DISALLOW_DEBUGGING_FEATURES);
        SharedPreferences.Editor e = p.edit().remove("restrictions").putBoolean("pendingDebugBlock", false);
        if (!saved.isEmpty()) e.putStringSet("lastRestrictions", saved);
        e.apply();
    }

    /**
     * Locks again from the phone, with the same parent code, server and restrictions as last time.
     * Blocks the network briefly (DNS check), so call it off the main thread. Returns null or an error.
     */
    static String relock(Context c) {
        if (!isOwner(c)) return "The app is not device owner.";
        SharedPreferences p = prefs(c);
        String dns = p.getString("dns", null);
        if (dns == null) dns = dpm(c).getGlobalPrivateDnsHost(admin(c));
        if (dns == null || !p.contains("hash")) return "No previous lock to restore. Lock from a computer.";
        String err = setDns(c, dns);
        if (err != null) return err.substring(0, 1).toUpperCase() + err.substring(1) + ".";

        Set<String> rs = new HashSet<>(p.getStringSet("lastRestrictions", new HashSet<String>()));
        if (rs.isEmpty()) {
            for (String[] n : NAMES) rs.add(n[0]);
        }
        rs.add(UserManager.DISALLOW_CONFIG_PRIVATE_DNS);
        for (String s : rs) dpm(c).addUserRestriction(admin(c), s);
        p.edit().putStringSet("restrictions", rs).remove("lastRestrictions")
                .putBoolean("pendingDebugBlock", false).apply();
        return null;
    }

    /** Unlocks, then gives up device owner: the app can be uninstalled. */
    @SuppressWarnings("deprecation")
    static void release(Context c) {
        unlock(c);
        if (isOwner(c)) dpm(c).clearDeviceOwnerApp(c.getPackageName());
        prefs(c).edit().clear().apply();
    }

    static String sha256(String s) {
        try {
            byte[] d = MessageDigest.getInstance("SHA-256").digest(s.getBytes(StandardCharsets.UTF_8));
            StringBuilder sb = new StringBuilder();
            for (byte b : d) sb.append(String.format("%02x", b));
            return sb.toString();
        } catch (Exception e) {
            throw new RuntimeException(e);
        }
    }
}
