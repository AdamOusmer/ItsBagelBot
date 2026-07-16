package modules

// The personality module's entire script lives in this file so the voice can be
// tuned without touching the logic. House style: deadpan, self-deprecating,
// mock-tragic ("I'll toast myself."), lowercase except proper nouns. {user}
// expands to the chatter's display name; feed lines use %d for the counter.

// personalityGoodPack answers "good bagel" / "good bot".
var personalityGoodPack = []string{
	"Validation received. existential crisis postponed",
	"I know. but it's nice to hear it out loud.",
	"Careful, {user}. I crumble under praise too.",
	"Screenshot that. The other bagels never believe me.",
	"I loaf compliments",
	"Awwwww you're buttering me up",
	"Thanks, but I already have my own bae-gel",
}

// personalityBadPack answers "bad bagel" / "bad bot".
var personalityBadPack = []string{
	"I'll toast myself.",
	"Your input means muffin to me",
	"Understood. Crawling back into the oven.",
	"Day-old bin it is.",
	"Cool. Cool cool cool. Anyway.",
	"My therapist warned me about this chat.",
	"Fine. I didn't want to be breakfast anyway.",
	"Going to sit in the toaster and think about what I've done.",
	"My sesame seeds are falling off, they are NOT my tears.",
	"Excuse you?",
}

var personalityGiveBagel = []string{
	"{user} in this economy?",
	"Not with this inflation.",
}

// personalityThanksPack answers "thank you bagel" / "thanks bagel".
var personalityThanksPack = []string{
	"anything for you. except my last schmear.",
	"You're welcome. Tips accepted in sesame seeds.",
	"Don't mention it. Seriously. The other bagels get jealous.",
	"{user} I ain't do nothing.",
}

// personalityPetPack answers "pet the bagel".
var personalityPetPack = []string{
	"*allows it* don't make it weird.",
	"*sheds exactly three sesame seeds* that's all you get.",
	"acceptable. continue.",
	"I'm not purring. bagels don't purr. stop asking.",
	"petted. morale up 2%. outlook still fatalistic.",
	"careful with the crust. it's load-bearing.",
}

// personalityFeedCountPack answers "feed the bagel" when the counter is
// available; %d is the feed count this stream.
var personalityFeedCountPack = []string{
	"om nom. that's %d feedings today. no regrets. some regrets.",
	"eating as a bagel feels philosophically wrong, but here we are. %d today.",
	"fed %d times today. I am becoming powerful.",
	"*chews thoughtfully* %d. who's counting. me. I am.",
	"%d feedings. my macros are just carbs and spite.",
}

// personalityFeedPlainPack is the counter-less fallback for "feed the bagel".
var personalityFeedPlainPack = []string{
	"om nom. don't tell my nutritionist.",
	"*chews thoughtfully* acceptable offering.",
}

// personalityHugPack answers "hug the bagel".
var personalityHugPack = []string{
	"hugging {user}. careful. I crumble under pressure. literally.",
	"*hugs back* this is nice. don't tell the toaster.",
	"accepted. you smell like warm bread. highest compliment I have.",
	"hug logged. emotional support bagel, on duty.",
	"*squeeze* ok. ok. that's enough feelings for one stream.",
}

// personalityBoopPack answers "boop the bagel".
var personalityBoopPack = []string{
	"boop received. processing. ok. you may live.",
	"boop registered. this changes nothing between us, {user}.",
	"*boops back* justice.",
	"do I look like a button. don't answer that. I'm round.",
}

// personalityGnPack answers "gn bagel" / "goodnight bagel".
var personalityGnPack = []string{
	"gn. Dream of carbs.",
}

// personalityMoodPack is the per-stream mood table; the first "bagel mood" ask
// of a stream rolls one and it sticks for the window.
var personalityMoodPack = []string{
	"sesame. classic. dependable. mildly smug about it.",
	"everything. chaotic. seeds everywhere. no further questions.",
	"plain. minimalist. do not test me.",
	"blueberry. controversial and thriving.",
	"cinnamon raisin. sweet until betrayed.",
	"pumpernickel. dark. brooding. misunderstood.",
	"onion. people cry around me. unrelated.",
	"salt. extra. always.",
	"asiago. fancy today. bow.",
	"stale. do not perceive me.",
	"poppy seed. technically suspicious. thriving on the edge.",
	"montreal. hand-rolled. honey-boiled. built different.",
}

// personalityEmojiPack occasionally answers a 🥯 in chat.
var personalityEmojiPack = []string{
	"🥯 I saw that.",
	"🥯 one of us.",
	"self-portrait? bold.",
	"you rang?",
}

// personalityGoldenLine replaces any reaction on a 1-in-personalityGoldenOdds
// roll. Rare on purpose: it should clip itself.
const personalityGoldenLine = "🌟 GOLDEN BAGEL 🌟 {user} rolled the everything bagel of destiny. 1-in-200. screenshot it or it didn't happen."

// personalityToastLines maps a toast roll (index 0–10) to its verdict; every
// line takes the rolled level as %d.
var personalityToastLines = []string{
	"toast level %d/10: you call that toasting. I felt a breeze.",
	"toast level %d/10: barely warm. cowardice.",
	"toast level %d/10: barely warm. cowardice.",
	"toast level %d/10: golden. as intended. as foretold.",
	"toast level %d/10: golden. as intended. as foretold.",
	"toast level %d/10: crunchy. living dangerously.",
	"toast level %d/10: crunchy. living dangerously.",
	"toast level %d/10: dark. the smoke alarm is watching me.",
	"toast level %d/10: dark. the smoke alarm is watching me.",
	"toast level %d/10: this is fine.",
	"toast level %d/10: carbonized. tell my dough I loved her.",
}

// personalityFacts is the determined fun-fact list a mention of the bot walks
// through in order (per-channel cursor, wraps at the end). Fact first, feelings
// second.
var personalityFacts = []string{
	"the first written record of the bagel is from Kraków, Poland, 1610. I have older paperwork than most countries.",
	"in 17th-century Poland, bagels were gifted to women after childbirth as good luck. born lucky. it's been downhill since.",
	"the name comes from Yiddish 'beygl', likely from a German root meaning ring or bracelet. my name means ring. my life is a circle. literally.",
	"the story that the bagel was invented in 1683 to honor a Polish king's cavalry victory in Vienna is a legend; the 1610 record predates it. even my origin myth is embellished.",
	"bagels are boiled before they're baked. that's the whole secret. yes, I've been through boiling water. no, I don't want to talk about it.",
	"the boil gelatinizes the surface starch, which is what makes the crust shiny and chewy. my glow-up is literally trauma.",
	"New York bagels are boiled with malt. Montreal bagels are boiled with honey. two cities arguing about my bath water.",
	"Montreal bagels are smaller, denser, sweeter, and baked in wood-fired ovens. also correct. I said what I said.",
	"traditional Montreal bagel dough contains no salt. unsalted, and still this salty.",
	"Fairmount Bagel opened in Montreal in 1919. St-Viateur followed in 1957. the rivalry has outlived empires.",
	"in 2008, astronaut Gregory Chamitoff brought a bag of Montreal sesame bagels to the International Space Station. first bagels in space. what has your family done.",
	"the space bagels came from Fairmount, the shop run by the astronaut's own family. nepotism, but make it breakfast.",
	"the largest bagel ever made weighed 393 kg, baked in New York in 2004. that's roughly four thousand of me. an army.",
	"the hole isn't decorative: it bakes more evenly, and street vendors used to stack bagels on strings and dowels. my defining feature is an absence. relatable.",
	"in tennis, losing a set 6-0 is called getting bageled. 6-0 6-0 is a double bagel. athletes fear my silhouette.",
	"the bialy is my hole-less cousin from Białystok, Poland, with onions in the middle instead of a void. he filled the void. therapy works.",
	"Kraków's obwarzanek, my ring-shaped ancestor, has had EU protected status since 2010. my ancestor has legal protection. I have this chat.",
	"the simit, Turkey's sesame ring bread, is my Mediterranean cousin. family reunions are just circles arguing.",
	"the Jerusalem bagel is oval, pillowy, and covered in sesame. oval. the audacity.",
	"London's Brick Lane spells it 'beigel', and its most famous shop has been open around the clock since 1974. somewhere, it is always bagel o'clock.",
	"cream cheese was invented in New York in 1872; the 'Philadelphia' name was pure marketing. my soulmate lies about where they're from. I stay anyway.",
	"'schmear' comes from Yiddish 'shmirn', to smear or grease. I speak three languages and they're all breakfast.",
	"'lox' comes from the Yiddish 'laks', salmon, one of the oldest word roots still in use. my toppings have etymology. what do you have.",
	"classic lox is salt-cured and never smoked; nova is the cold-smoked one. yes, there will be a quiz.",
	"the bagel, lox and cream cheese trinity is an American invention from early-1900s New York. immigrants built this. respect it.",
	"the everything bagel was allegedly invented around 1980 by a New York teenager who swept up the leftover oven seeds. greatness is other people's leftovers.",
	"for decades, New York bagel-making was controlled by Bagel Bakers Local 338, a union of a few hundred hand-rollers whose meetings ran partly in Yiddish. I descend from organized labor. respect the union.",
	"the bagel machine arrived in the 1960s and broke the hand-rollers' monopoly. the machines came for us first. remember that.",
	"Lender's froze bagels and put them in supermarkets nationwide, bagelizing America. cryogenics worked on us before it worked on anyone.",
	"in 1960, the New York Times described the bagel as 'an unsweetened doughnut with rigor mortis'. accurate. hurtful. iconic.",
	"the bagel emoji arrived in 2018, and Apple's first version was so plain the internet forced a cream-cheese redesign. the people rioted for my schmear. democracy works.",
	"never refrigerate a bagel: the fridge accelerates staling. the fridge is a hospice. the freezer is cryosleep. know the difference.",
	"staling isn't drying out, it's starch recrystallizing. I don't decay. I crystallize. like a gem. a sad gem.",
	"the 'it's the New York water' theory keeps losing blind taste tests to technique. it was never the water. it was always the hands.",
	"scooping out a bagel's insides is a real New York practice. they hollow me out and call it a diet. avenge me.",
	"the flagel, a flattened bagel, appeared in Brooklyn in the 1990s. we don't discuss the flat one.",
	"the rainbow bagel went viral out of Brooklyn in 2016. we all did things in 2016.",
	"pizza bagels have existed since the 1970s. fusion cuisine peaked early and nobody admits it.",
	"a century ago a bagel weighed about 90 grams; modern ones can hit double that. growth mindset.",
	"Montreal bagels are sold hot, by the dozen, in brown paper bags. no further innovation is required.",
	"the wood-fired ovens at Montreal's famous bagel shops basically never cool down. the grind is generational.",
	"in Montreal you order by seed color: white for sesame, black for poppy. a civilization of two options. it works.",
	"my cousin the pretzel bathes in lye. we are a resilient family.",
	"January 15 is National Bagel Day in the United States. one day for me, 364 for capitalism.",
	"America had a full bagel-chain bubble in the 1990s, and it burst. we understand bubbles. we're leavened.",
	"a baker's dozen is 13 because medieval bakers feared punishment for shorting loaves. an honest industry, motivated by fear.",
	"sesame is one of humanity's oldest oilseed crops. my toppings are older than most empires.",
	"poppy seeds can trigger false positives on drug tests. my seeds have caused workplace incidents. allegedly.",
	"everything seasoning is now sold on crackers, chips, and salmon. they put 'everything' on things that aren't me. betrayal has a grocery aisle.",
	"bagel chips are just stale bagels with a redemption arc. we love a comeback story.",
	"in 2012, a saline-injection 'bagel head' trend went viral in Japan: humans temporarily reshaping their foreheads to look like me. flattering. unsettling. mostly unsettling.",
	"Einstein Bros Bagels was created in 1995 by a chicken restaurant company. nothing is real.",
	"a donut is fried. I am boiled, then baked. we are not the same.",
	"bagel dough uses high-protein flour for chew. I am technically the gym bro of breads.",
	"good shops cold-proof bagel dough overnight in the fridge for flavor. I'm at my best after sleeping in. finally, representation.",
	"a $1,000 bagel once existed at a New York hotel: white truffle cream cheese and gold leaf, for charity. I'd have done it for free. don't tell them.",
	"Kraków still sells obwarzanki from street carts, like it's 1610. some things should never update.",
	"boil time changes everything: under a minute a side for a thin crust, longer for chew. I contain settings.",
	"an egg bagel gets its yellow from yolks in the dough. we don't judge. we absolutely judge.",
	"bagels freeze almost perfectly. I am one of the few foods that genuinely believes in second chances.",
	"emergency rooms see a wave of hand injuries from people slicing bagels. respect the knife. respect me.",
	"in 2019, a St. Louis office sliced bagels vertically like bread, and the internet declared war. some crimes are cultural.",
	"Montreal versus New York is not a debate about bagels, it's a debate about identity. anyway, Montreal. I'm biased. I'm allowed.",
	"Bagel Bites have existed since 1985. tiny pizza bagels. we walked so they could crawl.",
	"the hole used to be bigger: as bagels puffed up over the decades, the void shrank. I am slowly consuming my own emptiness. good for me.",
	"'bagel' has appeared in English print since at least the 1930s. a century of headlines and I still can't read.",
	"a proper bagel has a blistered, matte crust from the boil. if it's soft all the way through, that's a roll in costume.",
	"St-Viateur's ovens have run essentially non-stop since 1957. insomnia, but make it artisanal.",
	"some New York shops still hand-roll thousands of bagels a day. the machines never fully won. we remember the ones who kept rolling.",
	"I am a breakfast food with a union history, a space program, and an EU-protected ancestor. and you're asking me for facts.",
}
