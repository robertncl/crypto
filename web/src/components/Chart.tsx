import { useEffect, useRef, useState } from "react";
import {
  createChart, CandlestickSeries, HistogramSeries, ColorType, CrosshairMode,
  type IChartApi, type ISeriesApi, type UTCTimestamp,
} from "lightweight-charts";
import { api } from "../api/client";
import type { Candle, Market } from "../api/types";
import { useChannel } from "../hooks/useStream";
import { toNum } from "../utils/format";

const INTERVALS: { label: string; sec: number }[] = [
  { label: "1m", sec: 60 },
  { label: "5m", sec: 300 },
  { label: "15m", sec: 900 },
  { label: "1h", sec: 3600 },
  { label: "4h", sec: 14400 },
  { label: "1d", sec: 86400 },
];

const UP = "#0ecb81";
const DOWN = "#f6465d";

export function Chart({ market }: { market: Market }) {
  const wrapRef = useRef<HTMLDivElement>(null);
  const chartRef = useRef<IChartApi | null>(null);
  const candleRef = useRef<ISeriesApi<"Candlestick"> | null>(null);
  const volRef = useRef<ISeriesApi<"Histogram"> | null>(null);
  const [interval, setInterval] = useState("15m");

  const sec = INTERVALS.find((i) => i.label === interval)?.sec ?? 900;

  // Create the chart once.
  useEffect(() => {
    const el = wrapRef.current;
    if (!el) return;
    const chart = createChart(el, {
      autoSize: true,
      layout: {
        background: { type: ColorType.Solid, color: "transparent" },
        textColor: "#848e9c",
        fontFamily: "inherit",
      },
      grid: {
        vertLines: { color: "rgba(43,49,57,0.4)" },
        horzLines: { color: "rgba(43,49,57,0.4)" },
      },
      crosshair: { mode: CrosshairMode.Normal },
      rightPriceScale: { borderColor: "#2b3139" },
      timeScale: { borderColor: "#2b3139", timeVisible: true, secondsVisible: false },
    });
    const candles = chart.addSeries(CandlestickSeries, {
      upColor: UP, downColor: DOWN, borderVisible: false, wickUpColor: UP, wickDownColor: DOWN,
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

  // Load historical candles when market or interval changes.
  useEffect(() => {
    let live = true;
    api.candles(market.symbol, interval, 400).then((rows: Candle[]) => {
      if (!live || !candleRef.current || !volRef.current) return;
      candleRef.current.setData(
        rows.map((c) => ({
          time: c.time as UTCTimestamp,
          open: toNum(c.open), high: toNum(c.high), low: toNum(c.low), close: toNum(c.close),
        })),
      );
      volRef.current.setData(
        rows.map((c) => ({
          time: c.time as UTCTimestamp,
          value: toNum(c.volume),
          color: toNum(c.close) >= toNum(c.open) ? "rgba(14,203,129,0.4)" : "rgba(246,70,93,0.4)",
        })),
      );
      chartRef.current?.timeScale().fitContent();
    }).catch(() => {});
    return () => { live = false; };
  }, [market.symbol, interval]);

  // Live candle updates for the selected interval.
  useChannel<Candle>(`kline:${market.symbol}:${sec}`, (c) => {
    candleRef.current?.update({
      time: c.time as UTCTimestamp,
      open: toNum(c.open), high: toNum(c.high), low: toNum(c.low), close: toNum(c.close),
    });
    volRef.current?.update({
      time: c.time as UTCTimestamp,
      value: toNum(c.volume),
      color: toNum(c.close) >= toNum(c.open) ? "rgba(14,203,129,0.4)" : "rgba(246,70,93,0.4)",
    });
  });

  return (
    <section className="panel panel--chart">
      <header className="panel__head panel__head--chart">
        <h3>{market.base}/{market.quote}</h3>
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
