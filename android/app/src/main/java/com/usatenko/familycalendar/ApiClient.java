package com.usatenko.familycalendar;

import android.content.Context;

import org.json.JSONObject;

import java.io.BufferedReader;
import java.io.IOException;
import java.io.InputStream;
import java.io.InputStreamReader;
import java.io.OutputStream;
import java.net.URL;
import java.nio.charset.StandardCharsets;

import javax.net.ssl.HttpsURLConnection;

final class ApiClient {
    static final class Result {
        final String json;
        final String title;

        Result(String json, String title) {
            this.json = json;
            this.title = title;
        }
    }

    private ApiClient() {}

    static Result fetch(Context context, int widgetId) throws Exception {
        String token = WidgetPrefs.token(context);
        if (token.length() < 32) {
            throw new IOException("Откройте приложение и сохраните токен доступа");
        }
        String base = WidgetPrefs.serverUrl(context).trim();
        if (!base.startsWith("https://")) {
            throw new IOException("Разрешён только HTTPS-адрес");
        }
        while (base.endsWith("/")) {
            base = base.substring(0, base.length() - 1);
        }
        String day = WidgetPrefs.day(context, widgetId);
        String mode = WidgetPrefs.mode(context, widgetId);
        URL url = new URL(base + "/api/widget/schedule?day=" + day + "&mode=" + mode);
        HttpsURLConnection connection = (HttpsURLConnection) url.openConnection();
        connection.setConnectTimeout(5_000);
        connection.setReadTimeout(8_000);
        connection.setRequestMethod("GET");
        connection.setRequestProperty("Authorization", "Bearer " + token);
        connection.setRequestProperty("Accept", "application/json");
        try {
            int status = connection.getResponseCode();
            if (status != 200) {
                throw new IOException(status == 401 ? "Неверный токен доступа" : "Ошибка сервера: HTTP " + status);
            }
            String body = readAll(connection.getInputStream());
            JSONObject root = new JSONObject(body);
            return new Result(body, root.getString("title"));
        } finally {
            connection.disconnect();
        }
    }

    static String redeemPairingCode(String code) throws Exception {
        URL url = new URL(WidgetPrefs.DEFAULT_SERVER_URL + "/api/widget/pair");
        HttpsURLConnection connection = (HttpsURLConnection) url.openConnection();
        connection.setConnectTimeout(5_000);
        connection.setReadTimeout(8_000);
        connection.setRequestMethod("POST");
        connection.setDoOutput(true);
        connection.setRequestProperty("Content-Type", "application/json; charset=utf-8");
        connection.setRequestProperty("Accept", "application/json");
        byte[] payload = new JSONObject().put("code", code).toString().getBytes(StandardCharsets.UTF_8);
        try {
            try (OutputStream output = connection.getOutputStream()) {
                output.write(payload);
            }
            int status = connection.getResponseCode();
            if (status != 200) {
                throw new IOException(status == 410
                        ? "Ссылка уже использована или устарела"
                        : "Ошибка подключения: HTTP " + status);
            }
            JSONObject root = new JSONObject(readAll(connection.getInputStream()));
            String serverUrl = root.getString("server_url");
            String token = root.getString("token");
            if (!WidgetPrefs.DEFAULT_SERVER_URL.equals(serverUrl) || token.length() < 32) {
                throw new IOException("Сервер вернул некорректные настройки");
            }
            return token;
        } finally {
            connection.disconnect();
        }
    }

    private static String readAll(InputStream input) throws IOException {
        StringBuilder result = new StringBuilder();
        try (BufferedReader reader = new BufferedReader(new InputStreamReader(input, StandardCharsets.UTF_8))) {
            String line;
            while ((line = reader.readLine()) != null) {
                result.append(line);
            }
        }
        return result.toString();
    }
}
