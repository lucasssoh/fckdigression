package fr.lucas.dnslock;

import android.content.BroadcastReceiver;
import android.content.Context;
import android.content.Intent;

/**
 * Receives commands from the computer tool (adb shell am broadcast ...).
 * The manifest requires the DUMP permission: only the adb shell can call it.
 * The reply goes back in the broadcast data, which "am broadcast" prints.
 */
public class CommandReceiver extends BroadcastReceiver {

    private static final String P = "fr.lucas.dnslock.";

    @Override
    public void onReceive(Context c, Intent i) {
        String action = i.getAction() == null ? "" : i.getAction();
        String result;
        switch (action) {
            case P + "LOCK":
                result = Lock.applyLock(c,
                        i.getStringExtra("dns"),
                        i.getStringExtra("salt"),
                        i.getStringExtra("hash"),
                        i.getIntExtra("len", 0),
                        i.getBooleanExtra("keep_code", false),
                        i.getBooleanExtra("block_reset", true),
                        i.getBooleanExtra("block_vpn", true),
                        i.getBooleanExtra("block_debug", true));
                break;
            case P + "FINALIZE":
                result = Lock.finalizeLock(c);
                break;
            case P + "STATUS":
                result = Lock.status(c);
                break;
            case P + "UNLOCK":
                int r = Lock.checkPin(c, i.getStringExtra("pin"));
                if (r != Lock.PIN_OK) {
                    result = "ERROR: " + Lock.pinError(c, r);
                } else if (i.getBooleanExtra("release", false)) {
                    Lock.release(c);
                    result = "OK: lock removed, the app can be uninstalled";
                } else {
                    Lock.unlock(c);
                    result = "OK: unlocked";
                }
                break;
            default:
                result = "ERROR: unknown command";
        }
        setResultCode(result.startsWith("OK") || action.endsWith("STATUS") ? 1 : 2);
        setResultData(result);
    }
}
