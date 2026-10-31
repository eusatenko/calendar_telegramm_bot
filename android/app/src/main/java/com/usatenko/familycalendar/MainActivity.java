package com.usatenko.familycalendar;

import android.app.Activity;
import android.os.Bundle;
import android.widget.Button;
import android.widget.EditText;
import android.widget.TextView;

public final class MainActivity extends Activity {
    @Override
    protected void onCreate(Bundle state) {
        super.onCreate(state);
        setContentView(R.layout.activity_main);

        EditText server = findViewById(R.id.server_url);
        EditText token = findViewById(R.id.api_token);
        TextView status = findViewById(R.id.status);
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
    }
}
