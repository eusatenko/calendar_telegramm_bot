package com.usatenko.familycalendar;

import android.app.Activity;
import android.content.Intent;
import android.net.Uri;
import android.os.Bundle;
import android.widget.Button;
import android.widget.EditText;
import android.widget.TextView;

import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;

public final class MainActivity extends Activity {
    private final ExecutorService executor = Executors.newSingleThreadExecutor();
    private TextView status;

    @Override
    protected void onCreate(Bundle state) {
        super.onCreate(state);
        setContentView(R.layout.activity_main);

        EditText server = findViewById(R.id.server_url);
        EditText token = findViewById(R.id.api_token);
        status = findViewById(R.id.status);
        Button save = findViewById(R.id.save);

        server.setText(WidgetPrefs.serverUrl(this));
        token.setText(WidgetPrefs.token(this));
        save.setOnClickListener(view -> {
            String serverValue = server.getText().toString().trim();
            String tokenValue = token.getText().toString().trim();
            if (!serverValue.startsWith("https://")) {
                status.setText(R.string.https_required);
                return;
            }
            if (tokenValue.length() < 32) {
                status.setText(R.string.token_too_short);
                return;
            }
            WidgetPrefs.saveConnection(this, serverValue, tokenValue);
            status.setText(R.string.saved_updating);
            WidgetUpdater.refreshAll(this);
        });
        handleSetupIntent(getIntent());
    }

    @Override
    protected void onNewIntent(Intent intent) {
        super.onNewIntent(intent);
        setIntent(intent);
        handleSetupIntent(intent);
    }

    @Override
    protected void onDestroy() {
        executor.shutdown();
        super.onDestroy();
    }

    private void handleSetupIntent(Intent intent) {
        if (intent == null || !Intent.ACTION_VIEW.equals(intent.getAction())) {
            return;
        }
        Uri data = intent.getData();
        if (data == null) {
            return;
        }
        String code = data.getQueryParameter("code");
        if (code == null || code.length() < 32) {
            status.setText(R.string.invalid_pairing_link);
            return;
        }
        status.setText(R.string.pairing_in_progress);
        executor.execute(() -> {
            try {
                String pairedToken = ApiClient.redeemPairingCode(code);
                WidgetPrefs.saveConnection(this, WidgetPrefs.DEFAULT_SERVER_URL, pairedToken);
                WidgetUpdater.refreshAll(this);
                runOnUiThread(() -> status.setText(R.string.pairing_complete));
            } catch (Exception error) {
                String message = error.getMessage();
                runOnUiThread(() -> status.setText(message == null ? getString(R.string.pairing_failed) : message));
            }
        });
    }
}
