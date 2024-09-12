import axios from "axios";
import { motion } from "framer-motion";
import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { useProvider } from "../layouts/MainLayout";

export default function Home() {
    const [ongoings, setOngoings] = useState<{ loading?: boolean; data?: { title: string; url: string; type: string; image: string }[]; total?: number; error?: string }>();
    const [page, setPage] = useState(2);
    const [loadMoreLoading, setLoadMoreLoading] = useState(false);
    const [dataBefore, setDataBefore] = useState(0);
    const { provider } = useProvider();

    // if (!provider) return;

    useEffect(() => {
        if (!provider) return;
        (async () => {
            try {
                setOngoings((p) => ({ ...p, loading: true }));
                const { data } = await axios.get(`/api/${provider}/ongoing`);
                setOngoings((p) => ({ ...p, data: data.data, total: data.total }));
            } catch (error) {
                if (error instanceof Error) {
                    setOngoings((p) => ({ ...p, error: error.message }));
                }
            } finally {
                setOngoings((p) => ({ ...p, loading: false }));
            }
        })();
    }, [provider]);

    async function loadMore() {
        try {
            setDataBefore(ongoings?.data?.length ?? 0);
            setLoadMoreLoading(true);
            const { data } = await axios.get(`/api/${provider}/ongoing?page=` + page);
            setOngoings((p) => ({ ...p, data: [...(p?.data ?? []), ...data.data] }));
            setPage((p) => p + 1);
        } catch (error) {
            if (error instanceof Error) {
                console.log(error.message);
            }
        } finally {
            setLoadMoreLoading(false);
        }
    }

    return (
        <>
            <div className="container mx-auto mt-1 flex h-[20vh] items-center justify-center rounded-lg bg-white/10">
                <motion.p
                    animate={{
                        backgroundImage: ["linear-gradient(to right, teal, #38b2ac, #81e6d9)", "linear-gradient(to right, #81e6d9, #38b2ac, teal)"],
                    }}
                    transition={{
                        duration: 1, // Durasi animasi
                        repeat: Infinity, // Ulangi animasi terus-menerus
                        ease: "linear", // Animasi dengan kecepatan konstan
                    }}
                    style={{
                        backgroundSize: "200% 200%", // Ukuran gradien agar cukup besar untuk bergerak
                        backgroundClip: "text", // Hanya warna teks yang terpengaruh
                        WebkitBackgroundClip: "text", // Dukungan untuk WebKit
                        color: "transparent", // Teks transparan agar hanya gradien yang terlihat
                    }}
                    className="bg-gradient-to-r from-teal-700 to-teal-300 bg-clip-text text-xl font-semibold text-transparent"
                >
                    COMING SOON
                </motion.p>
            </div>

            <div className="container mx-auto mt-4 flex flex-row items-center rounded-lg border border-white/10">
                <div className="mr-4 h-10 w-1 rounded-lg bg-teal-500" />
                <p className="text-lg font-medium">
                    ON <span className="text-teal-500">GOING</span>
                </p>
            </div>

            {ongoings?.loading ? (
                <>
                    <div className="container mx-auto mt-16 flex items-center justify-center">
                        <motion.div animate={{ x: [100, 0, 100] }} transition={{ repeat: Infinity, duration: 2 }} className="h-2 w-12 rounded-lg bg-teal-500" />
                    </div>
                </>
            ) : (
                <>
                    <div className="container mx-auto mt-4">
                        <div className="grid grid-cols-3 gap-4 overflow-hidden md:grid-cols-4 lg:grid-cols-6 xl:grid-cols-8 2xl:grid-cols-10">
                            {ongoings?.data?.map((v, i) => {
                                return (
                                    <Link to={"/anime/" + v.url}>
                                        <motion.div initial={{ opacity: 0 }} animate={{ opacity: [0, 1], y: [20, 0] }} transition={{ delay: 0.1 * Math.abs(dataBefore - i) }} key={i} className="relative flex flex-col rounded-lg">
                                            <img src={v.image} alt="" className="rounded-lg" />
                                            <p className="line-clamp-1 break-words">{v.title}</p>
                                            <p className="font-xs absolute left-2 top-2 rounded-lg bg-teal-500 px-2 py-0 font-medium text-white">{v.type}</p>
                                        </motion.div>
                                    </Link>
                                );
                            })}
                        </div>
                    </div>

                    {/* PAGINATION */}
                    <div className="container mx-auto mt-4 flex flex-row items-center justify-center gap-4">
                        <button disabled={loadMoreLoading} onClick={loadMore} className="rounded-lg bg-white/10 px-4 py-1.5 font-medium text-teal-500">
                            LOAD MORE
                        </button>
                    </div>
                </>
            )}
            {/* <div className="relative h-48 w-32 overflow-hidden rounded-lg bg-zinc-950">
                    <motion.div className="absolute inset-0 bg-white/70" animate={{ opacity: [0, 1, 0] }} transition={{ duration: 1.5, repeat: Infinity }} />
                </div> */}
        </>
    );
}
