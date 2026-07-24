import { useEffect, useRef, useState } from "react";
import {
  createChart, CandlestickSeries, HistogramSeries, ColorType, CrosshairMode,
  type IChartApi, type ISeriesApi, type UTCTimestamp,
} from "lightweight-charts";
import { api } from "../api/client";
import type { Candle, Market } from "../api/types";
import { useChannel } from "../hooks/useStream";
import { useTheme } from "../hooks/useTheme";
import { toNum } from "../utils/format";

const INTERVALS: { label: string; sec: number }[] = [
  { label: "1m", sec: 60 },
  { label: "5m", sec: 300 },
  { label: "15m", sec: 900 },
  { label: "1h", sec: 3600 },
  { label: "4h", sec: 14400 },
  { label: "1d", sec: 86400 },
];

// Reads the live ACME design-system tokens off the root element so the
// chart follows the light/dark toggle without a lightweight-charts-specific
// color system of its own.
function acmeChartColors() {
  const s = getComputedStyle(document.documentElement);
  const v = (name: string) => s.getPropertyValue(name).trim();
  return {
    up: v("--acme-color-success") || "#22c55e",
    down: v("--acme-color-danger") || "#c8102e",
    text: v("--acme-color-text-muted") || "#6c7480",
    grid: v("--acme-color-border") || "#e4e7eb",
  };
}

function candlePoint(c: Candle) {
  return {
    time: c.time as UTCTimestamp,
    open: toNum(c.open), high: toNum(c.high), low: toNum(c.low), close: toNum(c.close),
  };
}

function volumePoint(c: Candle, colors: { up: string; down: string }) {
  return {
    time: c.time as UTCTimestamp,
    value: toNum(c.volume),
    color: toNum(c.close) >= toNum(c.open) ? `${colors.up}66` : `${colors.down}66`,
  };
}

export function Chart({ market }: { market: Market }) {
  const wrapRef = useRef<HTMLDivElement>(null);
  const chartRef = useRef<IChartApi | null>(null);
  const candleRef = useRef<ISeriesApi<"Candlestick"> | null>(null);
  const volRef = useRef<ISeriesApi<"Histogram"> | null>(null);
  const [interval, setInterval] = useState("15m");
  const [theme] = useTheme();

  const sec = INTERVALS.find((i) => i.label === interval)?.sec ?? 900;

  // Create the chart once.
  useEffect(() => {
    const el = wrapRef.current;
    if (!el) return;
    const c = acmeChartColors();
    const chart = createChart(el, {
      autoSize: true,
      layout: {
        background: { type: ColorType.Solid, color: "transparent" },
        textColor: c.text,
        fontFamily: "inherit",
      },
      grid: {
        vertLines: { color: c.grid },
        horzLines: { color: c.grid },
      },
      crosshair: { mode: CrosshairMode.Normal },
      rightPriceScale: { borderColor: c.grid },
      timeScale: { borderColor: c.grid, timeVisible: true, secondsVisible: false },
    });
    const candles = chart.addSeries(CandlestickSeries, {
      upColor: c.up, downColor: c.down, borderVisible: false, wickUpColor: c.up, wickDownColor: c.down,
    });
    const vol = chart.addSeries(HistogramSeries, {
      priceFormat: { type: "volume" },
      priceScaleId: "",
    });
    vol.priceScale().applyOptions({ scaleMargins: { top: 0.82, bottom: 0 } });

    chartRef.current = chart;
    candleRef.current = candles;
    volRef.current = vol;
    return () => {
      chart.remove();
      chartRef.current = null;
      candleRef.current = null;
      volRef.current = null;
    };
  }, []);

  // Follow the light/dark toggle without recreating the chart/series.
  useEffect(() => {
    const c = acmeChartColors();
    chartRef.current?.applyOptions({
      layout: { textColor: c.text },
      grid: { vertLines: { color: c.grid }, horzLines: { color: c.grid } },
      rightPriceScale: { borderColor: c.grid },
      timeScale: { borderColor: c.grid },
    });
    candleRef.current?.applyOptions({ upColor: c.up, downColor: c.down, wickUpColor: c.up, wickDownColor: c.down });
  }, [theme]);

  // Load historical candles when market or interval changes.
  useEffect(() => {
    let live = true;
    api.candles(market.symbol, interval, 400).then((rows: Candle[]) => {
      if (!live || !candleRef.current || !volRef.current) return;
      const colors = acmeChartColors();
      candleRef.current.setData(rows.map(candlePoint));
      volRef.current.setData(rows.map((row) => volumePoint(row, colors)));
      chartRef.current?.timeScale().fitContent();
    }).catch(() => {});
    return () => { live = false; };
  }, [market.symbol, interval]);

  // Live candle updates for the selected interval.
  useChannel<Candle>(`kline:${market.symbol}:${sec}`, (c) => {
    const colors = acmeChartColors();
    candleRef.current?.update(candlePoint(c));
    volRef.current?.update(volumePoint(c, colors));
  });

  return (
    <section className="panel panel--chart">
      <header className="panel__head panel__head--chart">
        <h2>{market.base}/{market.quote}</h2>
        <div className="seg">
          {INTERVALS.map((i) => (
            <button
              key={i.label}
              className={`seg__btn ${i.label === interval ? "is-active" : ""}`}
              onClick={() => setInterval(i.label)}
            >
              {i.label}
            </button>
          ))}
        </div>
      </header>
      <div className="chart" ref={wrapRef} />
    </section>
  );
}
