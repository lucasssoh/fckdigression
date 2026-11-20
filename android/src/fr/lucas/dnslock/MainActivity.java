package fr.lucas.dnslock;

import android.app.Activity;
import android.app.AlertDialog;
import android.content.res.Configuration;
import android.graphics.Color;
import android.graphics.Typeface;
import android.graphics.drawable.Drawable;
import android.graphics.drawable.GradientDrawable;
import android.os.Bundle;
import android.os.Handler;
import android.os.Looper;
import android.os.UserManager;
import android.text.TextUtils;
import android.view.Gravity;
import android.view.HapticFeedbackConstants;
import android.view.View;
import android.view.ViewGroup;
import android.view.WindowManager;
import android.widget.ImageView;
import android.widget.LinearLayout;
import android.widget.ScrollView;
import android.widget.TextView;

/**
 * The on-phone screen: lock status, parent-code entry, lock again, remove.
 * Plain Android views built in code (no AndroidX, no layouts), so the build stays a few aapt2/d8 calls.
 */
public class MainActivity extends Activity {

    private static final int MODE_UNLOCK = 0;
    private static final int MODE_RELEASE = 1;

    // palette, set in onCreate from the system light/dark mode
    private int bg, fg, muted, card, line, accent, onAccent, heroLocked, onHeroLocked,
            heroOpen, onHeroOpen, pillOnBg, pillOnFg, pillOffBg, pillOffFg, danger;
    private boolean dark;

    private final Handler handler = new Handler(Looper.getMainLooper());
    private Runnable ticker;

    /** True once the parent code was accepted while this screen is open (Remove needs no second entry). */
    private boolean codeAccepted;

    // PIN screen state
    private final StringBuilder digits = new StringBuilder();
    private boolean showDigits;
    private int pinMode;
    private LinearLayout groupsBox;
    private TextView counter, pinError, eye;

    @Override
    protected void onCreate(Bundle b) {
        super.onCreate(b);
        getWindow().addFlags(WindowManager.LayoutParams.FLAG_SECURE); // no screenshots / recents preview
        dark = (getResources().getConfiguration().uiMode & Configuration.UI_MODE_NIGHT_MASK)
                == Configuration.UI_MODE_NIGHT_YES;
        if (dark) {
            bg = 0xFF0F1515; fg = 0xFFE2EAE8; muted = 0xFF8FA09D; card = 0xFF182221; line = 0xFF26332F;
            accent = 0xFF4DB3A8; onAccent = 0xFF08201E; heroLocked = 0xFF0F5C5A; onHeroLocked = 0xFFF2FBFA;
            heroOpen = 0xFF3A2E14; onHeroOpen = 0xFFF6E2B4; pillOnBg = 0xFF1D3432; pillOnFg = 0xFF7FD0C6;
            pillOffBg = 0xFF3A2E14; pillOffFg = 0xFFEDC473; danger = 0xFFE77C68;
        } else {
            bg = 0xFFF6F8F7; fg = 0xFF142120; muted = 0xFF5D6B69; card = 0xFFFFFFFF; line = 0xFFDEE5E3;
            accent = 0xFF0F5C5A; onAccent = 0xFFFFFFFF; heroLocked = 0xFF0F5C5A; onHeroLocked = 0xFFF2FBFA;
            heroOpen = 0xFFF3E3BF; onHeroOpen = 0xFF3D2B05; pillOnBg = 0xFFDCEBE9; pillOnFg = 0xFF0F5C5A;
            pillOffBg = 0xFFF3E3BF; pillOffFg = 0xFF6B4A08; danger = 0xFFB3402E;
        }
        getWindow().setStatusBarColor(bg);
        getWindow().setNavigationBarColor(bg);
        if (!dark) {
            getWindow().getDecorView().setSystemUiVisibility(
                    View.SYSTEM_UI_FLAG_LIGHT_STATUS_BAR | View.SYSTEM_UI_FLAG_LIGHT_NAVIGATION_BAR);
        }
        showHome(null);
    }

    @Override
    protected void onResume() {
        super.onResume();
        // State may have changed from adb while the screen was in the background.
        if (pinMode < 0) showHome(null);
    }

    @Override
    protected void onDestroy() {
        stopTicker();
        super.onDestroy();
    }

    @Override
    public void onBackPressed() {
        if (pinMode >= 0) showHome(null); else super.onBackPressed();
    }

    // ------------------------------------------------------------------ home

    private void showHome(String message) {
        stopTicker();
        pinMode = -1;
        if (!Lock.isOwner(this)) {
            showNotSetUp(message);
            return;
        }
        boolean locked = Lock.isLocked(this);
        LinearLayout root = page();

        root.addView(appTitle());

        // hero
        LinearLayout hero = column(dp(18));
        hero.setBackground(round(locked ? heroLocked : heroOpen, 22));
        int heroFg = locked ? onHeroLocked : onHeroOpen;
        hero.addView(text(locked ? "Protected" : "Unlocked", 28, heroFg, true));
        TextView sub = text(locked ? "Private DNS is locked to"
                : "Settings can be changed. Private DNS is still set to", 14, heroFg, false);
        sub.setAlpha(0.85f);
        sub.setPadding(0, dp(4), 0, dp(8));
        hero.addView(sub);
        String host = Lock.dnsHost(this);
        TextView chip = text(host == null ? "not set" : host, 13, heroFg, false);
        chip.setTypeface(Typeface.MONOSPACE);
        chip.setPadding(dp(8), dp(4), dp(8), dp(4));
        chip.setBackground(round(locked ? 0x24FFFFFF : 0x14000000, 8));
        hero.addView(chip, wrap());
        root.addView(hero, matchW(dp(18)));

        // DNS guard
        LinearLayout label = new LinearLayout(this);
        TextView l1 = text("DNS GUARD", 12, muted, true);
        l1.setLetterSpacing(0.08f);
        label.addView(l1, new LinearLayout.LayoutParams(0, -2, 1));
        label.addView(text(locked ? "on" : "paused", 12, muted, false));
        label.setPadding(dp(4), 0, dp(4), dp(8));
        root.addView(label, matchW(0));

        LinearLayout list = column(0);
        list.setBackground(stroke(card, line, 16));
        String[][] rows = {
                {UserManager.DISALLOW_CONFIG_PRIVATE_DNS, "Private DNS changes"},
                {UserManager.DISALLOW_CONFIG_VPN, "VPN apps"},
                {UserManager.DISALLOW_FACTORY_RESET, "Factory reset"},
                {UserManager.DISALLOW_DEBUGGING_FEATURES, "USB debugging"},
                {UserManager.DISALLOW_ADD_USER, "Extra users"},
        };
        for (int k = 0; k < rows.length; k++) {
            if (k > 0) list.addView(divider());
            list.addView(statusRow(rows[k][1], Lock.has(this, rows[k][0])));
        }
        root.addView(list, matchW(dp(16)));

        if (message != null) {
            TextView m = text(message, 14, fg, false);
            m.setPadding(dp(4), 0, dp(4), dp(16));
            root.addView(m);
        }

        root.addView(spacer());

        if (locked) {
            root.addView(button("Enter parent code", accent, onAccent, v -> showPin(MODE_UNLOCK)), matchW(0));
        } else {
            TextView note = text("Lock again reuses the same parent code, DNS server and blocks as last time. "
                    + "To change them, run fckd lock from a computer.", 13, muted, false);
            note.setPadding(dp(4), 0, dp(4), dp(12));
            root.addView(note);
            root.addView(button("Lock again", accent, onAccent, v -> relock()), matchW(dp(4)));
            root.addView(button("Remove fckdigression from this phone…", Color.TRANSPARENT, danger,
                    v -> { if (codeAccepted) confirmRelease(); else showPin(MODE_RELEASE); }), matchW(0));
        }
        setPage(root);
    }

    private void relock() {
        final AlertDialog wait = new AlertDialog.Builder(this)
                .setMessage("Locking… checking the DNS server.").setCancelable(false).show();
        new Thread(() -> {
            String err = Lock.relock(this);
            runOnUiThread(() -> {
                wait.dismiss();
                codeAccepted = false;
                showHome(err == null ? "Locked again." : err);
            });
        }).start();
    }

    private void confirmRelease() {
        new AlertDialog.Builder(this)
                .setTitle("Remove fckdigression?")
                .setMessage("The app gives up device-owner rights and can then be uninstalled.\n\n"
                        + "Locking again will need a phone with no accounts, so in practice a factory reset.")
                .setPositiveButton("Remove", (d, w) -> {
                    Lock.release(this);
                    codeAccepted = false;
                    showHome("Removed. You can uninstall the app now.");
                })
                .setNegativeButton("Cancel", null)
                .show();
    }

    // ------------------------------------------------------------------ not set up

    private void showNotSetUp(String message) {
        LinearLayout root = page();
        root.addView(appTitle());

        LinearLayout hero = column(dp(18));
        GradientDrawable d = stroke(card, muted, 22);
        d.setStroke(dp(1), muted, dp(5), dp(4));
        hero.setBackground(d);
        hero.addView(text("Not set up", 26, fg, true));
        TextView s = text("This app needs device-owner rights, which it can only get from a computer.",
                14, muted, false);
        s.setPadding(0, dp(6), 0, 0);
        hero.addView(s);
        root.addView(hero, matchW(dp(20)));

        String[] steps = {
                "Remove every account from the phone.",
                "Turn on USB debugging and connect the phone.",
                "On the computer, run:",
        };
        for (int k = 0; k < steps.length; k++) {
            LinearLayout row = new LinearLayout(this);
            row.setGravity(Gravity.CENTER_VERTICAL);
            TextView n = text(String.valueOf(k + 1), 12, fg, true);
            n.setGravity(Gravity.CENTER);
            n.setBackground(oval(line));
            row.addView(n, new LinearLayout.LayoutParams(dp(24), dp(24)));
            TextView t = text(steps[k], 14, fg, false);
            t.setPadding(dp(12), 0, 0, 0);
            row.addView(t);
            root.addView(row, matchW(dp(10)));
        }
        TextView cmd = text("fckd lock --dns <your-server>", 13, fg, false);
        cmd.setTypeface(Typeface.MONOSPACE);
        cmd.setPadding(dp(12), dp(10), dp(12), dp(10));
        cmd.setBackground(round(line, 8));
        root.addView(cmd, matchW(dp(16)));

        if (message != null) root.addView(text(message, 14, fg, false));
        setPage(root);
    }

    // ------------------------------------------------------------------ PIN

    private void showPin(int mode) {
        stopTicker();
        pinMode = mode;
        digits.setLength(0);
        showDigits = false;
        LinearLayout root = page();

        TextView back = text("←  Parent code", 18, fg, true);
        back.setPadding(0, dp(4), 0, dp(20));
        back.setOnClickListener(v -> showHome(null));
        root.addView(back);

        long wait = Lock.waitMs(this);
        if (wait > 0) {
            buildLockout(root);
        } else {
            TextView t = text(mode == MODE_RELEASE ? "Enter the parent code to remove the app"
                    : "Enter the parent code", 18, fg, true);
            t.setGravity(Gravity.CENTER);
            root.addView(t, matchW(dp(4)));
            TextView s = text("Have it read out to you in groups of 5", 13, muted, false);
            s.setGravity(Gravity.CENTER);
            root.addView(s, matchW(dp(16)));

            groupsBox = column(0);
            root.addView(groupsBox, matchW(dp(8)));

            LinearLayout meta = new LinearLayout(this);
            counter = text("", 13, muted, false);
            counter.setFontFeatureSettings("tnum");
            meta.addView(counter, new LinearLayout.LayoutParams(0, -2, 1));
            eye = text("Show digits", 13, accent, true);
            eye.setPadding(dp(8), dp(4), 0, dp(4));
            eye.setOnClickListener(v -> { showDigits = !showDigits; renderDigits(); });
            meta.addView(eye);
            root.addView(meta, matchW(dp(8)));

            int left = Lock.attemptsLeft(this);
            String hint = left + (left == 1 ? " attempt" : " attempts") + " left";
            if (Lock.codeLength(this) > 0) hint += " · a wrong length doesn't count";
            TextView h = text(hint, 12, muted, false);
            h.setGravity(Gravity.CENTER);
            root.addView(h, matchW(dp(6)));

            pinError = text("", 13, danger, true);
            pinError.setGravity(Gravity.CENTER);
            root.addView(pinError, matchW(0));

            root.addView(spacer());
            root.addView(keypad(true), matchW(0));
            renderDigits();
        }
        setPage(root);
    }

    private void buildLockout(LinearLayout root) {
        TextView t = text("Too many wrong codes", 18, fg, true);
        t.setGravity(Gravity.CENTER);
        root.addView(t, matchW(dp(4)));
        TextView s = text("Try again in", 13, muted, false);
        s.setGravity(Gravity.CENTER);
        root.addView(s, matchW(dp(8)));
        final TextView timer = text("", 44, fg, true);
        timer.setGravity(Gravity.CENTER);
        timer.setFontFeatureSettings("tnum");
        root.addView(timer, matchW(dp(8)));
        TextView n = text("Each new mistake doubles the wait, up to 24 h. Entries of the wrong length are never counted.",
                13, muted, false);
        n.setGravity(Gravity.CENTER);
        root.addView(n, matchW(0));
        root.addView(spacer());
        root.addView(keypad(false), matchW(0));

        // A lambda, not an inner class: d8 8.2 crashes on inner-class constructors compiled by recent JDKs.
        ticker = () -> tickTimer(timer);
        ticker.run();
    }

    /** Updates the wait timer every second; reopens the keypad when the wait is over. */
    private void tickTimer(TextView timer) {
        long ms = Lock.waitMs(this);
        if (ms <= 0) { showPin(pinMode); return; }
        long sec = ms / 1000;
        timer.setText(sec >= 3600
                ? String.format("%d:%02d:%02d", sec / 3600, sec / 60 % 60, sec % 60)
                : String.format("%02d:%02d", sec / 60, sec % 60));
        if (ticker != null) handler.postDelayed(ticker, 1000);
    }

    private void renderDigits() {
        int len = Lock.codeLength(this);
        int n = digits.length();
        int groups = len > 0 ? (len + 4) / 5 : Math.max(2, n / 5 + 1);
        groupsBox.removeAllViews();
        LinearLayout row = null;
        for (int g = 0; g < groups; g++) {
            if (g % 3 == 0) {
                row = new LinearLayout(this);
                groupsBox.addView(row, matchW(dp(6)));
            }
            int from = g * 5, to = Math.min(n, from + 5);
            StringBuilder s = new StringBuilder();
            for (int k = from; k < to; k++) s.append(showDigits ? digits.charAt(k) : '•');
            boolean current = n >= from && n < from + 5 && (len == 0 || n < len);
            if (current) s.append('_');
            TextView cell = text(s.toString(), 18, fg, false);
            cell.setTypeface(Typeface.MONOSPACE);
            cell.setGravity(Gravity.CENTER);
            cell.setLetterSpacing(0.05f);
            cell.setPadding(0, dp(8), 0, dp(8));
            GradientDrawable d = stroke(card, current ? accent : line, 10);
            if (current) d.setStroke(dp(2), accent);
            cell.setBackground(d);
            LinearLayout.LayoutParams lp = new LinearLayout.LayoutParams(0, dp(44), 1);
            if (g % 3 != 0) lp.leftMargin = dp(6);
            row.addView(cell, lp);
        }
        // pad the last row so cells keep the same width
        while (row != null && row.getChildCount() < 3) {
            View v = new View(this);
            LinearLayout.LayoutParams lp = new LinearLayout.LayoutParams(0, dp(44), 1);
            lp.leftMargin = dp(6);
            row.addView(v, lp);
        }
        counter.setText(len > 0 ? n + " / " + len + " digits" : n + (n == 1 ? " digit" : " digits"));
        eye.setText(showDigits ? "Hide digits" : "Show digits");
    }

    private LinearLayout keypad(boolean enabled) {
        LinearLayout pad = column(0);
        String[] keys = {"1", "2", "3", "4", "5", "6", "7", "8", "9", "⌫", "0", "OK"};
        for (int r = 0; r < 4; r++) {
            LinearLayout row = new LinearLayout(this);
            for (int k = 0; k < 3; k++) {
                final String key = keys[r * 3 + k];
                boolean fn = key.equals("⌫") || key.equals("OK");
                TextView b = text(key, fn ? 18 : 24, key.equals("OK") ? accent : fn ? muted : fg, key.equals("OK"));
                b.setGravity(Gravity.CENTER);
                if (!fn) b.setBackground(stroke(card, line, 14));
                LinearLayout.LayoutParams lp = new LinearLayout.LayoutParams(0, dp(58), 1);
                if (k > 0) lp.leftMargin = dp(8);
                row.addView(b, lp);
                if (!enabled) { b.setAlpha(0.35f); continue; }
                b.setOnClickListener(v -> { v.performHapticFeedback(HapticFeedbackConstants.KEYBOARD_TAP); press(key); });
                if (key.equals("⌫")) b.setOnLongClickListener(v -> { digits.setLength(0); renderDigits(); return true; });
            }
            pad.addView(row, matchW(r < 3 ? dp(8) : 0));
        }
        return pad;
    }

    private void press(String key) {
        int len = Lock.codeLength(this);
        pinError.setText("");
        if (key.equals("⌫")) {
            if (digits.length() > 0) digits.setLength(digits.length() - 1);
        } else if (key.equals("OK")) {
            submit();
            return;
        } else if (digits.length() < (len > 0 ? len : Lock.MAX_CODE_LEN)) {
            digits.append(key);
        }
        renderDigits();
    }

    private void submit() {
        int len = Lock.codeLength(this);
        if (digits.length() == 0) return;
        if (len > 0 && digits.length() != len) {
            pinError.setText((len - digits.length()) + " digits missing. Nothing was counted.");
            return;
        }
        String code = digits.toString();
        digits.setLength(0);
        int r = Lock.checkPin(this, code);
        if (r != Lock.PIN_OK) {
            if (Lock.waitMs(this) > 0) { showPin(pinMode); return; }
            renderDigits();
            pinError.setText(Lock.pinError(this, r));
            return;
        }
        codeAccepted = true;
        if (pinMode == MODE_RELEASE) {
            Lock.unlock(this);
            showHome(null);
            confirmRelease();
        } else {
            Lock.unlock(this);
            showHome("Unlocked. Settings can be changed until you lock again.");
        }
    }

    // ------------------------------------------------------------------ view helpers

    private LinearLayout page() {
        LinearLayout root = column(0);
        root.setPadding(dp(20), dp(16), dp(20), dp(20));
        root.setMinimumHeight(getResources().getDisplayMetrics().heightPixels - dp(48));
        return root;
    }

    private void setPage(LinearLayout root) {
        ScrollView sv = new ScrollView(this);
        sv.setFillViewport(true);
        sv.setBackgroundColor(bg);
        sv.addView(root, new ViewGroup.LayoutParams(-1, -2));
        setContentView(sv);
    }

    private View appTitle() {
        LinearLayout row = new LinearLayout(this);
        row.setGravity(Gravity.CENTER_VERTICAL);
        ImageView icon = new ImageView(this);
        Drawable d = getApplicationInfo().loadIcon(getPackageManager());
        icon.setImageDrawable(d);
        row.addView(icon, new LinearLayout.LayoutParams(dp(30), dp(30)));
        TextView t = text("fckdigression", 20, fg, true);
        t.setPadding(dp(10), 0, 0, 0);
        row.addView(t);
        row.setPadding(0, dp(4), 0, dp(18));
        return row;
    }

    private View statusRow(String label, boolean blocked) {
        LinearLayout row = new LinearLayout(this);
        row.setGravity(Gravity.CENTER_VERTICAL);
        row.setPadding(dp(14), dp(12), dp(14), dp(12));
        row.addView(text(label, 15, muted, false), new LinearLayout.LayoutParams(0, -2, 1));
        TextView pill = text(blocked ? "Blocked" : "Allowed", 12, blocked ? pillOnFg : pillOffFg, true);
        pill.setPadding(dp(10), dp(3), dp(10), dp(3));
        pill.setBackground(round(blocked ? pillOnBg : pillOffBg, 99));
        row.addView(pill);
        return row;
    }

    private View divider() {
        View v = new View(this);
        v.setBackgroundColor(line);
        v.setLayoutParams(new LinearLayout.LayoutParams(-1, Math.max(1, dp(1) / 2 + 1)));
        return v;
    }

    private TextView button(String label, int bgColor, int fgColor, View.OnClickListener l) {
        TextView b = text(label, 15, fgColor, true);
        b.setGravity(Gravity.CENTER);
        b.setPadding(dp(16), dp(14), dp(16), dp(14));
        b.setBackground(round(bgColor, 99));
        b.setOnClickListener(l);
        return b;
    }

    private TextView text(String s, float sp, int color, boolean bold) {
        TextView t = new TextView(this);
        t.setText(s);
        t.setTextSize(sp);
        t.setTextColor(color);
        t.setTypeface(Typeface.create(bold ? "sans-serif-medium" : "sans-serif", Typeface.NORMAL));
        t.setEllipsize(TextUtils.TruncateAt.END);
        return t;
    }

    private LinearLayout column(int pad) {
        LinearLayout l = new LinearLayout(this);
        l.setOrientation(LinearLayout.VERTICAL);
        l.setPadding(pad, pad, pad, pad);
        return l;
    }

    private View spacer() {
        View v = new View(this);
        v.setLayoutParams(new LinearLayout.LayoutParams(-1, 0, 1));
        v.setMinimumHeight(dp(16));
        return v;
    }

    private LinearLayout.LayoutParams matchW(int bottom) {
        LinearLayout.LayoutParams lp = new LinearLayout.LayoutParams(-1, -2);
        lp.bottomMargin = bottom;
        return lp;
    }

    private LinearLayout.LayoutParams wrap() {
        return new LinearLayout.LayoutParams(-2, -2);
    }

    private GradientDrawable round(int color, int radiusDp) {
        GradientDrawable d = new GradientDrawable();
        d.setColor(color);
        d.setCornerRadius(dp(radiusDp));
        return d;
    }

    private GradientDrawable stroke(int color, int strokeColor, int radiusDp) {
        GradientDrawable d = round(color, radiusDp);
        d.setStroke(dp(1), strokeColor);
        return d;
    }

    private GradientDrawable oval(int color) {
        GradientDrawable d = new GradientDrawable();
        d.setShape(GradientDrawable.OVAL);
        d.setColor(color);
        return d;
    }

    private void stopTicker() {
        if (ticker != null) handler.removeCallbacks(ticker);
        ticker = null;
    }

    private int dp(int v) {
        return Math.round(v * getResources().getDisplayMetrics().density);
    }
}
