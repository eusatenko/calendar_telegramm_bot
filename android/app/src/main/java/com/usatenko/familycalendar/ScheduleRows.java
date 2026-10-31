package com.usatenko.familycalendar;

import android.content.Context;
import android.view.View;
import android.widget.RemoteViews;

import org.json.JSONArray;
import org.json.JSONObject;

import java.util.ArrayList;
import java.util.List;

final class ScheduleRows {
    static final class Row {
        final String kind;
        final String text;
        final String location;

        Row(String kind, String text, String location) {
            this.kind = kind;
            this.text = text;
            this.location = location;
        }
    }

    private ScheduleRows() {}

    static List<Row> read(Context context, int widgetId) {
        List<Row> rows = new ArrayList<>();
        String cached = WidgetPrefs.cache(context, widgetId);
        if (cached.isEmpty()) {
            return rows;
        }
        try {
            JSONArray values = new JSONObject(cached).getJSONArray("rows");
            for (int i = 0; i < values.length(); i++) {
                JSONObject value = values.getJSONObject(i);
                rows.add(new Row(value.getString("kind"), value.getString("text"),
                        value.optString("location", "")));
            }
        } catch (Exception ignored) {
            rows.clear();
        }
        return rows;
    }

    static RemoteViews remoteView(Context context, Row row) {
        if ("header".equals(row.kind)) {
            RemoteViews views = new RemoteViews(context.getPackageName(), R.layout.widget_row_header);
            views.setTextViewText(R.id.row_header, row.text);
            return views;
        }
        RemoteViews views = new RemoteViews(context.getPackageName(), R.layout.widget_row_event);
        views.setTextViewText(R.id.row_text, row.text);
        views.setTextViewText(R.id.row_location, row.location);
        views.setViewVisibility(R.id.row_location, row.location.isEmpty() ? View.GONE : View.VISIBLE);
        return views;
    }
}
